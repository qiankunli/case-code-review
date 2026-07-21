package unit

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/qiankunli/case-code-review/internal/model"
)

// tsAST uses the target project's TypeScript compiler to extract function
// boundaries. Resolving from the changed file's directory lets monorepos keep
// TypeScript in a package-level node_modules instead of requiring a global
// install. The compiler handles JavaScript and JSX as well as TypeScript.
const tsAST = `
const path = require("node:path");
const file = process.argv[1];

let ts;
try {
  const resolved = require.resolve("typescript", {
    paths: [path.dirname(path.resolve(file)), process.cwd()],
  });
  ts = require(resolved);
} catch {
  process.exit(3);
}

const chunks = [];
process.stdin.setEncoding("utf8");
process.stdin.on("data", (chunk) => chunks.push(chunk));
process.stdin.on("end", () => {
  const text = chunks.join("");
  const kind = file.endsWith(".tsx") ? ts.ScriptKind.TSX
    : file.endsWith(".jsx") ? ts.ScriptKind.JSX
    : file.endsWith(".js") || file.endsWith(".mjs") || file.endsWith(".cjs") ? ts.ScriptKind.JS
    : ts.ScriptKind.TS;
  const source = ts.createSourceFile(file, text, ts.ScriptTarget.Latest, true, kind);
  if (source.parseDiagnostics && source.parseDiagnostics.length > 0) process.exit(2);

  const entries = [];
  const nameOf = (name) => {
    if (!name) return "";
    if (ts.isIdentifier(name) || ts.isPrivateIdentifier(name) ||
        ts.isStringLiteral(name) || ts.isNumericLiteral(name)) return name.text;
    return "";
  };
  const add = (sym, node) => {
    if (!sym || !node) return;
    const start = source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
    const end = source.getLineAndCharacterOfPosition(Math.max(node.getStart(source), node.getEnd() - 1)).line + 1;
    entries.push({ sym, start, end, node });
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
  const collectObject = (object, prefix) => {
    for (const prop of object.properties) {
      const name = nameOf(prop.name);
      if (!name) continue;
      if (ts.isMethodDeclaration(prop) && prop.body) add(prefix + "." + name, prop);
      else if (ts.isPropertyAssignment(prop)) {
        const fn = functionLike(prop.initializer);
        if (fn) add(prefix + "." + name, prop);
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
        for (const member of statement.members) {
          if (ts.isConstructorDeclaration(member) && member.body) add(owner + ".constructor", member);
          else if ((ts.isMethodDeclaration(member) || ts.isGetAccessorDeclaration(member) ||
                    ts.isSetAccessorDeclaration(member)) && member.body) {
            add(owner + "." + nameOf(member.name), member);
          } else if (ts.isPropertyDeclaration(member) && member.initializer) {
            const fn = functionLike(member.initializer);
            if (fn) add(owner + "." + nameOf(member.name), member);
          }
        }
        continue;
      }
      if (ts.isVariableStatement(statement)) {
        for (const declaration of statement.declarationList.declarations) {
          if (!ts.isIdentifier(declaration.name) || !declaration.initializer) continue;
          const sym = prefix + declaration.name.text;
          const fn = functionLike(declaration.initializer);
          if (fn) add(sym, declaration);
          else if (ts.isObjectLiteralExpression(declaration.initializer)) collectObject(declaration.initializer, sym);
        }
        continue;
      }
      if (ts.isModuleDeclaration(statement) && statement.name) {
        let body = statement.body;
        while (body && ts.isModuleDeclaration(body)) body = body.body;
        if (body && ts.isModuleBlock(body)) collectStatements(body.statements, prefix + nameOf(statement.name) + ".");
      }
    }
  };
  collectStatements(source.statements);

  const callees = {};
  for (const entry of entries) {
    const names = [];
    const seen = new Set();
    const walk = (node) => {
      if (ts.isCallExpression(node)) {
        const expr = node.expression;
        const name = ts.isIdentifier(expr) ? expr.text
          : ts.isPropertyAccessExpression(expr) ? expr.name.text
          : ts.isElementAccessExpression(expr) && ts.isStringLiteral(expr.argumentExpression)
            ? expr.argumentExpression.text : "";
        if (name && !seen.has(name)) {
          seen.add(name);
          names.push(name);
        }
      }
      ts.forEachChild(node, walk);
    };
    walk(entry.node);
    callees[entry.sym] = names;
  }
  const spans = entries.map(({ sym, start, end }) => ({ sym, start, end }));
  process.stdout.write(JSON.stringify({ spans, callees }));
});
`

type tsAnalysis struct {
	spans   []funcSpan
	callees map[string][]string
}

// A CLI review parses the same post-change file from several consumers
// (splitter, briefing, comment tagging, and callgraph). Cache by source content
// so those consumers share one compiler invocation without risking stale spans.
var tsAnalysisCache sync.Map

// TSFuncSplitter cuts JavaScript and TypeScript-family files into one fragment
// per touched function, method, or named arrow function. It uses the target
// project's TypeScript compiler and falls back to file scope when Node or the
// compiler is unavailable, or when the source cannot be parsed.
type TSFuncSplitter struct {
	RepoDir string
}

func (s TSFuncSplitter) Split(d model.Diff) ([]Fragment, error) {
	if !isTSPath(d.NewPath) || d.NewFileContent == "" {
		return FileSplitter{}.Split(d)
	}
	spans, err := parseTSFuncsInRepo(s.RepoDir, d.NewPath, d.NewFileContent)
	if err != nil {
		return FileSplitter{}.Split(d)
	}
	return splitByFuncSpans(d, spans), nil
}

func isTSPath(path string) bool {
	for _, suffix := range []string{".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs"} {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

func parseTSFuncs(path, src string) ([]funcSpan, error) {
	return parseTSFuncsInRepo("", path, src)
}

func parseTSFuncsInRepo(repoDir, path, src string) ([]funcSpan, error) {
	analysis, err := analyzeTS(repoDir, path, src)
	if err != nil {
		return nil, err
	}
	return analysis.spans, nil
}

func analyzeTS(repoDir, path, src string) (tsAnalysis, error) {
	digest := sha256.Sum256([]byte(src))
	key := repoDir + "\x00" + path + "\x00" + string(digest[:])
	if cached, ok := tsAnalysisCache.Load(key); ok {
		return cached.(tsAnalysis), nil
	}
	out, err := runTSAST(repoDir, path, src)
	if err != nil {
		return tsAnalysis{}, err
	}
	var payload struct {
		Spans []struct {
			Sym   string `json:"sym"`
			Start int    `json:"start"`
			End   int    `json:"end"`
		} `json:"spans"`
		Callees map[string][]string `json:"callees"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return tsAnalysis{}, err
	}
	spans := make([]funcSpan, 0, len(payload.Spans))
	for _, item := range payload.Spans {
		spans = append(spans, funcSpan{start: item.Start, end: item.End, id: FuncID(path, "", item.Sym)})
	}
	analysis := tsAnalysis{spans: spans, callees: payload.Callees}
	tsAnalysisCache.Store(key, analysis)
	return analysis, nil
}

func runTSAST(repoDir, path, src string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "-e", tsAST, path)
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	cmd.Stdin = strings.NewReader(src)
	return cmd.Output()
}

func TSFuncIDAt(path, src string, line int) (string, bool) {
	return TSFuncIDAtInRepo("", path, src, line)
}

func TSFuncIDAtInRepo(repoDir, path, src string, line int) (string, bool) {
	spans, err := parseTSFuncsInRepo(repoDir, path, src)
	if err != nil {
		return "", false
	}
	for _, span := range spans {
		if line >= span.start && line <= span.end {
			return span.id, true
		}
	}
	return "", false
}

func TSCalleesOf(path, src, symbol string) []string {
	return TSCalleesOfInRepo("", path, src, symbol)
}

func TSCalleesOfInRepo(repoDir, path, src, symbol string) []string {
	analysis, err := analyzeTS(repoDir, path, src)
	if err != nil {
		return nil
	}
	names := analysis.callees[symbol]
	if len(names) == 0 {
		return nil
	}
	return names
}
