package codegraph

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TypeScript backend, same low-confidence shape as ScanGo and ScanPy. It uses
// the target project's TypeScript compiler so monorepos do not need a global
// install, and one Node process walks the whole repo to keep startup bounded.
// JavaScript and JSX use the same compiler frontend.
//
// Best-effort: absent Node/TypeScript, a timeout, or bad output degrade to nil.

const tsScanTimeout = 30 * time.Second

const tsScanScript = `
const fs = require("node:fs");
const path = require("node:path");

const root = path.resolve(process.argv[1]);
const SKIP_DIRS = new Set(["vendor", "node_modules", "testdata", "__pycache__", "venv", "site-packages"]);
const EXTENSIONS = [".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"];
const MAX_FILES = 2000;
const MAX_BYTES = 512 * 1024;
const out = {};
const compilers = new Map();
let count = 0;
let sawSource = false;
let usableCompiler = false;

const isSource = (name) => EXTENSIONS.some((ext) => name.endsWith(ext));
const isTest = (name) => /(^|[._-])(test|spec)\.(ts|tsx|js|jsx|mjs|cjs)$/.test(name) || /^test[_-]/.test(name);
const compilerFor = (file) => {
  let resolved;
  try {
    resolved = require.resolve("typescript", { paths: [path.dirname(file), root, process.cwd()] });
  } catch {
    return undefined;
  }
  if (!compilers.has(resolved)) {
    const compiler = require(resolved);
    const usable = compiler && typeof compiler.createSourceFile === "function" &&
      compiler.ScriptTarget && compiler.ScriptKind;
    compilers.set(resolved, usable ? compiler : undefined);
  }
  const compiler = compilers.get(resolved);
  if (compiler) usableCompiler = true;
  return compiler;
};

function scanFile(file) {
  let text;
  try {
    const stat = fs.statSync(file);
    if (stat.size > MAX_BYTES) return;
    text = fs.readFileSync(file, "utf8");
  } catch {
    return;
  }
  const ts = compilerFor(file);
  if (!ts) return;
  const scriptKind = file.endsWith(".tsx") ? ts.ScriptKind.TSX
    : file.endsWith(".jsx") ? ts.ScriptKind.JSX
    : file.endsWith(".js") || file.endsWith(".mjs") || file.endsWith(".cjs") ? ts.ScriptKind.JS
    : ts.ScriptKind.TS;
  const isPrivateIdentifier = (node) =>
    typeof ts.isPrivateIdentifier === "function" && ts.isPrivateIdentifier(node);
  const nameOf = (name) => {
    if (!name) return "";
    if (ts.isIdentifier(name) || isPrivateIdentifier(name) ||
        ts.isStringLiteral(name) || ts.isNumericLiteral(name)) return name.text;
    return "";
  };
  const functionLike = (node) => {
    while (node && (ts.isParenthesizedExpression(node) || ts.isAsExpression(node) ||
      ts.isTypeAssertionExpression(node) || ts.isNonNullExpression(node) ||
      (ts.isSatisfiesExpression && ts.isSatisfiesExpression(node)))) node = node.expression;
    if (node && (ts.isArrowFunction(node) || ts.isFunctionExpression(node))) return node;
    if (node && ts.isCallExpression(node)) {
      for (const arg of node.arguments) {
        const found = functionLike(arg);
        if (found) return found;
      }
    }
    return undefined;
  };
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, scriptKind);
  if (source.parseDiagnostics && source.parseDiagnostics.length > 0) return;

  const rel = path.relative(root, file).split(path.sep).join("/");
  const defs = [];
  const signatureOf = (node, fn) => {
    const start = node.getStart(source);
    let end = node.getEnd();
    if (fn && fn.body) {
      end = fn.body.getStart(source);
    } else {
      const body = text.slice(start, end);
      const brace = body.indexOf("{");
      if (brace >= 0) end = start + brace;
      else {
        const newline = body.indexOf("\n");
        if (newline >= 0) end = start + newline;
      }
    }
    const signature = text.slice(start, end).replace(/\s+/g, " ").trim();
    return signature.length > 240 ? signature.slice(0, 237) + "..." : signature;
  };
  const add = (sym, node, signatureNode = node, fn = node) => {
    if (!sym || !node) return;
    const line = source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
    defs.push({ ident: sym, symbol: rel + "::" + sym, line, sig: signatureOf(signatureNode, fn) });
  };
  const collectObject = (object, prefix) => {
    for (const prop of object.properties) {
      const name = nameOf(prop.name);
      if (!name) continue;
      if (ts.isMethodDeclaration(prop) && prop.body) add(prefix + "." + name, prop);
      else if (ts.isPropertyAssignment(prop)) {
        const fn = functionLike(prop.initializer);
        if (fn) add(prefix + "." + name, prop, prop, fn);
      }
    }
  };
  const collectStatements = (statements, prefix = "") => {
    for (const statement of statements) {
      if (ts.isFunctionDeclaration(statement) && statement.name && statement.body) {
        add(prefix + statement.name.text, statement);
        continue;
      }
      if (ts.isClassDeclaration(statement) && statement.name) {
        const owner = prefix + statement.name.text;
        add(owner, statement, statement, undefined);
        for (const member of statement.members) {
          if (ts.isConstructorDeclaration(member) && member.body) add(owner + ".constructor", member);
          else if ((ts.isMethodDeclaration(member) || ts.isGetAccessorDeclaration(member) ||
                    ts.isSetAccessorDeclaration(member)) && member.body) {
            add(owner + "." + nameOf(member.name), member);
          } else if (ts.isPropertyDeclaration(member) && member.initializer) {
            const fn = functionLike(member.initializer);
            if (fn) add(owner + "." + nameOf(member.name), member, member, fn);
          }
        }
        continue;
      }
      if (ts.isInterfaceDeclaration(statement) || ts.isTypeAliasDeclaration(statement) ||
          ts.isEnumDeclaration(statement)) {
        add(prefix + statement.name.text, statement, statement, undefined);
        continue;
      }
      if (ts.isVariableStatement(statement)) {
        for (const declaration of statement.declarationList.declarations) {
          if (!ts.isIdentifier(declaration.name) || !declaration.initializer) continue;
          const sym = prefix + declaration.name.text;
          const fn = functionLike(declaration.initializer);
          if (fn) add(sym, declaration, statement, fn);
          else if (ts.isObjectLiteralExpression(declaration.initializer)) collectObject(declaration.initializer, sym);
        }
        continue;
      }
      if (ts.isModuleDeclaration(statement) && statement.name) {
        const owner = prefix + nameOf(statement.name);
        add(owner, statement, statement, undefined);
        let body = statement.body;
        while (body && ts.isModuleDeclaration(body)) body = body.body;
        if (body && ts.isModuleBlock(body)) collectStatements(body.statements, owner + ".");
      }
    }
  };
  collectStatements(source.statements);

  const refs = {};
  const walkRefs = (node) => {
    if ((ts.isIdentifier(node) || isPrivateIdentifier(node)) && node.text.length >= 3) {
      refs[node.text] = (refs[node.text] || 0) + 1;
    }
    ts.forEachChild(node, walkRefs);
  };
  walkRefs(source);
  out[rel] = { defs, refs };
}

function walk(dir) {
  if (count >= MAX_FILES) return;
  let entries;
  try {
    entries = fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name));
  } catch {
    return;
  }
  for (const entry of entries) {
    if (count >= MAX_FILES) return;
    const file = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!entry.name.startsWith(".") && !SKIP_DIRS.has(entry.name)) walk(file);
      continue;
    }
    if (!entry.isFile() || !isSource(entry.name) || isTest(entry.name)) continue;
    sawSource = true;
    count++;
    scanFile(file);
  }
}

walk(root);
if (sawSource && !usableCompiler) process.exit(3);
process.stdout.write(JSON.stringify(out));
`

type tsFileScan struct {
	Defs []struct {
		Ident  string `json:"ident"`
		Symbol string `json:"symbol"`
		Line   int    `json:"line"`
		Sig    string `json:"sig"`
	} `json:"defs"`
	Refs map[string]int `json:"refs"`
}

// ScanTS extracts defs/refs from TypeScript, JavaScript, TSX, and JSX files.
// It returns nil when Node or the target project's TypeScript compiler is not
// available, so callers can continue with the other language backends.
func ScanTS(repoDir string) *Extraction {
	ex, _ := scanTS(repoDir)
	return ex
}

func scanTS(repoDir string) (*Extraction, error) {
	root, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve TypeScript scan root %q: %w", repoDir, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), tsScanTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "node", "-e", tsScanScript, root)
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	outBytes, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("scan TypeScript repo %q: %w: %s", root, err, strings.TrimSpace(stderr.String()))
	}
	var raw map[string]tsFileScan
	if err := json.Unmarshal(outBytes, &raw); err != nil {
		return nil, fmt.Errorf("decode TypeScript scan for %q: %w", root, err)
	}
	ex := &Extraction{Defs: map[string][]Def{}, Refs: map[string]map[string]int{}}
	for rel, fs := range raw {
		for _, d := range fs.Defs {
			ex.Defs[rel] = append(ex.Defs[rel], Def{
				Ident:     d.Ident,
				SymbolID:  d.Symbol,
				File:      rel,
				Line:      d.Line,
				Signature: d.Sig,
			})
		}
		if len(fs.Refs) > 0 {
			ex.Refs[rel] = fs.Refs
		}
	}
	return ex, nil
}
