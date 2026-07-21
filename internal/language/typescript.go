package language

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// typescriptAnalysisScript is the temporary legacy backend. It intentionally
// lives behind Analyzer: replacing it with gotreesitter must not affect any
// consumer package.
const typescriptAnalysisScript = `
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
if (!ts || typeof ts.createSourceFile !== "function" || !ts.ScriptTarget || !ts.ScriptKind) process.exit(3);
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
  const isPrivateIdentifier = (node) =>
    typeof ts.isPrivateIdentifier === "function" && ts.isPrivateIdentifier(node);
  const nameOf = (name) => {
    if (!name) return "";
    if (ts.isIdentifier(name) || isPrivateIdentifier(name) ||
        ts.isStringLiteral(name) || ts.isNumericLiteral(name)) return name.text;
    return "";
  };
  const add = (sym, node) => {
    if (!sym || !node) return;
    const start = source.getLineAndCharacterOfPosition(node.getStart(source)).line + 1;
    const end = source.getLineAndCharacterOfPosition(Math.max(node.getStart(source), node.getEnd() - 1)).line + 1;
    const signature = text.slice(node.getStart(source), text.indexOf("\n", node.getStart(source)) < 0
      ? node.getEnd() : Math.min(node.getEnd(), text.indexOf("\n", node.getStart(source)))).trim();
    entries.push({ sym, start, end, signature, node });
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

  const calls = [];
  for (const entry of entries) {
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
          calls.push({ caller: entry.sym, name });
        }
      }
      ts.forEachChild(node, walk);
    };
    walk(entry.node);
  }
  const references = {};
  const walkReferences = (node) => {
    if ((ts.isIdentifier(node) || isPrivateIdentifier(node)) && node.text.length >= 3) {
      references[node.text] = (references[node.text] || 0) + 1;
    }
    ts.forEachChild(node, walkReferences);
  };
  walkReferences(source);
  process.stdout.write(JSON.stringify({
    definitions: entries.map(({ sym, start, end, signature }) => ({ sym, start, end, signature })),
    calls,
    references,
  }));
});
`

func (a *Analyzer) analyzeTypeScript(parent context.Context, lang Language, source Source) (Analysis, error) {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "node", "-e", typescriptAnalysisScript, source.Path)
	if a.repoDir != "" {
		cmd.Dir = a.repoDir
	}
	cmd.Stdin = strings.NewReader(source.Content)
	out, err := cmd.Output()
	if err != nil {
		return Analysis{}, err
	}
	var payload struct {
		Definitions []struct {
			Symbol    string `json:"sym"`
			Start     int    `json:"start"`
			End       int    `json:"end"`
			Signature string `json:"signature"`
		} `json:"definitions"`
		Calls []struct {
			Caller string `json:"caller"`
			Name   string `json:"name"`
		} `json:"calls"`
		References map[string]int `json:"references"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Analysis{}, err
	}
	analysis := Analysis{Language: lang, Quality: QualitySyntax, References: payload.References}
	for _, d := range payload.Definitions {
		owner := ""
		kind := KindFunction
		if i := strings.LastIndex(d.Symbol, "."); i >= 0 {
			owner, kind = d.Symbol[:i], KindMethod
		}
		analysis.Definitions = append(analysis.Definitions, Definition{
			SymbolID: SymbolID(source.Path, "", d.Symbol), Name: d.Symbol, Owner: owner,
			Kind: kind, Span: Span{Start: d.Start, End: d.End}, Signature: d.Signature,
		})
	}
	for _, call := range payload.Calls {
		analysis.Calls = append(analysis.Calls, Call{
			CallerID: SymbolID(source.Path, "", call.Caller), Name: call.Name,
		})
	}
	return analysis, nil
}
