package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// A read is one place that reads a variable.
type read struct {
	name   string
	where  string // file:line
	reader string // the process that reads it, or "" when the path names none
}

// envName is what a variable's name looks like.
var envName = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// readerFuncs are the names a read goes through in Go: main passes os.Getenv or os.LookupEnv in
// as getenv or lookup, and settings read through an or(name, default) closure.
var readerFuncs = map[string]bool{"getenv": true, "lookup": true, "or": true}

// osReaders are the standard library's reads.
var osReaders = map[string]bool{"Getenv": true, "LookupEnv": true}

const secretRefPrefix = "env:"

// readerOf names the process whose code is at p, or "".
func readerOf(p string) string {
	for _, d := range processDirs {
		if strings.HasPrefix(p, d.dir) {
			return d.reader
		}
	}

	return ""
}

// scanGo finds the reads in one Go package: its files by path, all in one directory.
func scanGo(files map[string][]byte) ([]read, error) {
	fset := token.NewFileSet()

	parsed := make(map[string]*ast.File, len(files))
	for name, src := range files {
		f, err := parser.ParseFile(fset, name, src, parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}

		parsed[name] = f
	}

	consts := constants(parsed)

	var reads []read

	at := func(name string, pos token.Pos) read {
		p := fset.Position(pos)
		return read{name: name, where: fmt.Sprintf("%s:%d", p.Filename, p.Line), reader: readerOf(p.Filename)}
	}

	for _, f := range parsed {
		ast.Inspect(f, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && isReaderCall(c) && len(c.Args) > 0 {
				if s, ok := consts.eval(c.Args[0]); ok && envName.MatchString(s) {
					reads = append(reads, at(s, c.Pos()))
				}
			}

			return true
		})
	}

	// A constant "env:NAME" is a Secrets reference that the env adapter resolves from the environment.
	for _, id := range consts.idents {
		if s, ok := consts.eval(id); ok {
			if ref, ok := strings.CutPrefix(s, secretRefPrefix); ok && envName.MatchString(ref) {
				reads = append(reads, at(ref, id.Pos()))
			}
		}
	}

	return reads, nil
}

func isReaderCall(c *ast.CallExpr) bool {
	switch fn := c.Fun.(type) {
	case *ast.Ident:
		return readerFuncs[fn.Name]
	case *ast.SelectorExpr:
		x, ok := fn.X.(*ast.Ident)
		return ok && x.Name == "os" && osReaders[fn.Sel.Name]
	default:
		return false
	}
}

// constTable holds a package's string constants: their expressions, evaluated on demand.
type constTable struct {
	exprs  map[string]ast.Expr
	idents []*ast.Ident // every constant, as declared
}

func constants(files map[string]*ast.File) constTable {
	t := constTable{exprs: map[string]ast.Expr{}}

	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			d, ok := n.(*ast.GenDecl)
			if !ok || d.Tok != token.CONST {
				return true
			}

			for _, s := range d.Specs {
				vs, ok := s.(*ast.ValueSpec)
				if !ok || len(vs.Values) != len(vs.Names) {
					continue
				}

				for i, id := range vs.Names {
					t.exprs[id.Name] = vs.Values[i]
					t.idents = append(t.idents, id)
				}
			}

			return true
		})
	}

	return t
}

func (t constTable) eval(e ast.Expr) (string, bool) { return t.evalDepth(e, 0) }

// maxDepth bounds the constants a constant may refer through, so a cycle cannot hang the check
// (the compiler refuses one anyway).
const maxDepth = 16

// evalDepth evaluates a string literal, a constant's name, or a concatenation of them.
func (t constTable) evalDepth(e ast.Expr, depth int) (string, bool) {
	if depth > maxDepth {
		return "", false
	}

	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}

		s, err := strconv.Unquote(e.Value)

		return s, err == nil
	case *ast.Ident:
		x, ok := t.exprs[e.Name]
		if !ok {
			return "", false
		}

		return t.evalDepth(x, depth+1)
	case *ast.ParenExpr:
		return t.evalDepth(e.X, depth+1)
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}

		a, ok := t.evalDepth(e.X, depth+1)
		if !ok {
			return "", false
		}

		b, ok := t.evalDepth(e.Y, depth+1)

		return a + b, ok
	default:
		return "", false
	}
}

// The TypeScript reads. get('X') and env.X take only names with the shape of a variable, so a
// header lookup (get('user-agent')) or a method (env.get) does not count; get must be a plain call,
// not a method (headers.get('X_Y')).
var tsPatterns = []*regexp.Regexp{
	regexp.MustCompile(`\benv\.([A-Z][A-Z0-9_]*)\b`),
	regexp.MustCompile(`\benv\[\s*['"]([A-Z][A-Z0-9_]*)['"]\s*\]`),
	regexp.MustCompile(`(?:^|[^.\w$])get\(\s*'([A-Z][A-Z0-9]*_[A-Z0-9_]+)'\s*\)`),
	regexp.MustCompile(`\brequired\(\s*[\w.]+\s*,\s*'([A-Z][A-Z0-9_]*)'\s*\)`),
}

// scanTS finds the reads in one TypeScript file, skipping comment lines.
func scanTS(name string, src []byte) []read {
	var reads []read

	reader := readerOf(name)

	for i, line := range strings.Split(string(src), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*") {
			continue
		}

		for _, re := range tsPatterns {
			for _, m := range re.FindAllStringSubmatch(line, -1) {
				reads = append(reads, read{name: m[1], where: fmt.Sprintf("%s:%d", name, i+1), reader: reader})
			}
		}
	}

	return reads
}

func isTSSource(p string) bool {
	if path.Ext(p) != ".ts" || strings.HasSuffix(p, ".test.ts") || strings.Contains(p, "/testing/") {
		return false
	}

	for _, d := range tsDirs {
		if strings.HasPrefix(p, d) {
			return true
		}
	}

	return false
}

func isGoSource(p string) bool {
	return path.Ext(p) == ".go" && !strings.HasSuffix(p, "_test.go") &&
		!strings.HasPrefix(p, "testdata/") && !strings.Contains(p, "/testdata/")
}

// wrangler is what the check needs from wrangler.jsonc.
type wrangler struct {
	Vars                map[string]any `json:"vars"`
	SecretsStoreSecrets []struct {
		Binding string `json:"binding"`
	} `json:"secrets_store_secrets"`
	DurableObjects struct {
		Bindings []struct {
			Name string `json:"name"`
		} `json:"bindings"`
	} `json:"durable_objects"`
}

// parseWrangler reads wrangler.jsonc: JSON with comments and trailing commas.
func parseWrangler(src []byte) (wrangler, error) {
	var w wrangler
	if err := json.Unmarshal(jsonc(src), &w); err != nil {
		return wrangler{}, fmt.Errorf("parse %s: %w", wranglerFile, err)
	}

	return w, nil
}

// reads are the control Worker's settings: each var and each Secrets Store binding.
func (w wrangler) reads() []read {
	var reads []read

	for name := range w.Vars {
		reads = append(reads, read{name: name, where: wranglerFile + " vars", reader: readerControlWorker})
	}

	for _, s := range w.SecretsStoreSecrets {
		reads = append(reads, read{name: s.Binding, where: wranglerFile + " secrets_store_secrets", reader: readerControlWorker})
	}

	return reads
}

// bindings are names on env that are not variables (Durable Object namespaces).
func (w wrangler) bindings() map[string]bool {
	m := map[string]bool{}
	for _, b := range w.DurableObjects.Bindings {
		m[b.Name] = true
	}

	return m
}

// jsonc removes comments, then trailing commas, outside strings.
func jsonc(src []byte) []byte {
	var noComments bytes.Buffer

	walkJSON(src, func(i int, inString bool) int {
		switch {
		case !inString && bytes.HasPrefix(src[i:], []byte("//")):
			if end := bytes.IndexByte(src[i:], '\n'); end >= 0 {
				return i + end
			}

			return len(src)
		case !inString && bytes.HasPrefix(src[i:], []byte("/*")):
			if end := bytes.Index(src[i+2:], []byte("*/")); end >= 0 {
				return i + 2 + end + 2
			}

			return len(src)
		default:
			noComments.WriteByte(src[i])
			return i + 1
		}
	})

	clean := noComments.Bytes()

	var out bytes.Buffer

	walkJSON(clean, func(i int, inString bool) int {
		if !inString && clean[i] == ',' {
			if t := bytes.TrimLeft(clean[i+1:], " \t\r\n"); len(t) > 0 && (t[0] == '}' || t[0] == ']') {
				return i + 1
			}
		}

		out.WriteByte(clean[i])

		return i + 1
	})

	return out.Bytes()
}

// walkJSON calls step at each byte with whether it is inside a string (its quotes included);
// step returns the index to continue at.
func walkJSON(src []byte, step func(i int, inString bool) int) {
	inString, escaped := false, false

	for i := 0; i < len(src); {
		c := src[i]
		inside := inString || c == '"'

		switch {
		case !inString:
			inString = c == '"'
		case escaped:
			escaped = false
		case c == '\\':
			escaped = true
		case c == '"':
			inString = false
		}

		next := step(i, inside)
		if next > i+1 {
			inString, escaped = false, false // skipped bytes are never inside a string
		}

		i = next
	}
}

// dollarRef is a ${NAME} reference, as compose.yaml interpolates and the Taskfile's shell expands.
var dollarRef = regexp.MustCompile(`\$\{([A-Z][A-Z0-9_]*)`)

// scanDollar finds the ${NAME} references in a compose file or a Taskfile.
func scanDollar(name string, src []byte, reader string) []read {
	var reads []read

	for i, line := range strings.Split(string(src), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}

		for _, m := range dollarRef.FindAllStringSubmatch(line, -1) {
			reads = append(reads, read{name: m[1], where: fmt.Sprintf("%s:%d", name, i+1), reader: reader})
		}
	}

	return reads
}
