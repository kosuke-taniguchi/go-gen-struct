package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"html/template"
	"log"
	"os"
	"path/filepath"
	"strings"
)

var targetFields = []string{"CreatedAt", "UpdatedAt"}

// 1. 全ての.goファイルを取得
// 2. ファイルを解析してgen:generateコメントがついた構造体を取得
// 3. 対象の構造体がCreatedAt, UpdatedAtを持っていればSetCreatedAt, SetUpdatedAtを生成
func main() {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	files, err := listGoFiles(dir)
	if err != nil {
		panic(err)
	}
	for _, file := range files {
		targetStructs, err := searchTargetStructs(file)
		if err != nil {
			log.Println(err.Error()) // 他ファイルの解析に影響しなたいめにログだけ出す
			continue
		}
		if err := targetStructs.generateTargetSetter(targetFields); err != nil {
			log.Println(err.Error())
		}
	}
	log.Println("Successfully generated")
}

func listGoFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			files = append(files, path)
		}
		return nil
	})
	return files, err
}

// searchTargetStructs gen:generateコメントがついた構造体を探す
func searchTargetStructs(filename string) (*targetStructs, error) {
	fileSet := token.NewFileSet()
	node, err := parser.ParseFile(fileSet, filename, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	var structs []*ast.TypeSpec
	var imports []string
	ast.Inspect(node, func(n ast.Node) bool {
		genDecl, ok := n.(*ast.GenDecl)
		if !ok {
			return true
		}
		// 対象はcommentのついた構造体のみ
		if genDecl.Tok != token.TYPE || genDecl.Doc == nil {
			return true
		}
		imports = make([]string, 0, len(node.Imports))
		for _, importSpec := range node.Imports {
			imports = append(imports, importSpec.Path.Value[1:len(importSpec.Path.Value)-1])
			if err != nil {
				return true
			}
		}
		structs = make([]*ast.TypeSpec, 0, len(genDecl.Doc.List))
		for _, comment := range genDecl.Doc.List {
			if strings.HasPrefix(comment.Text, "//gen:setters") {
				for _, spec := range genDecl.Specs {
					typeSpec, ok := spec.(*ast.TypeSpec)
					if !ok {
						continue
					}
					if _, ok := typeSpec.Type.(*ast.StructType); ok {
						structs = append(structs, typeSpec)
					}
				}
			}
		}
		return true
	})
	return &targetStructs{
		structs:     structs,
		packageName: node.Name.Name,
		imports:     imports,
		path:        filepath.Dir(filename),
		filename:    filepath.Base(filename),
	}, nil
}

type targetStructs struct {
	path        string
	filename    string
	packageName string
	imports     []string
	structs     []*ast.TypeSpec
}

type templateData struct {
	PackageName string
	Imports     []string
	Setters     []*setter
}

type setter struct {
	StructName string
	FieldName  string
	FieldType  string
}

type usedImport struct {
	pkg  string
	used bool
}

func (t *targetStructs) generateTargetSetter(targets []string) error {
	// key: short package name, value: full package name
	importsMap := make(map[string]*usedImport, len(t.imports))
	for _, imp := range t.imports {
		importsMap[filepath.Base(imp)] = &usedImport{pkg: imp}
	}
	var setters []*setter
	imports := make([]string, 0, len(importsMap))
	for _, s := range t.structs {
		structType, ok := s.Type.(*ast.StructType)
		if !ok {
			continue
		}
		for _, field := range structType.Fields.List {
			if len(field.Names) == 0 {
				continue
			}
			fieldName := field.Names[0].Name
			if !containsTargetField(fieldName, targets...) {
				continue
			}
			// setterメソッドの生成
			fieldType := getFiledTypeString(field.Type)
			if strings.Contains(fieldType, ".") {
				pkg := strings.Split(fieldType, ".")[0]
				if _, ok := importsMap[pkg]; ok {
					importsMap[pkg].used = true
				}
			}
			setters = append(setters, &setter{
				StructName: s.Name.Name,
				FieldName:  fieldName,
				FieldType:  fieldType,
			})
		}
	}
	if len(setters) == 0 {
		return nil
	}
	for _, imp := range importsMap {
		if imp.used {
			imports = append(imports, imp.pkg)
		}
	}
	tmpl, err := template.New("goCode").Parse(setterTemplate)
	if err != nil {
		return err
	}
	buf := &bytes.Buffer{}
	err = tmpl.Execute(buf, &templateData{
		PackageName: t.packageName,
		Imports:     imports,
		Setters:     setters,
	})
	if err != nil {
		return err
	}
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		return err
	}
	outputPath := filepath.Join(
		t.path,
		fmt.Sprintf("%s_setters.go", strings.TrimSuffix(t.filename, ".go")),
	)
	if err := os.WriteFile(outputPath, formatted, 0644); err != nil {
		return err
	}
	return nil
}

func containsTargetField(f string, targets ...string) bool {
	for _, target := range targets {
		if f == target {
			return true
		}
	}
	return false
}

func getFiledTypeString(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.StarExpr:
		return "*" + getFiledTypeString(expr.X)
	case *ast.SelectorExpr:
		return getFiledTypeString(expr.X) + "." + expr.Sel.Name
	case *ast.ArrayType:
		return "[]" + getFiledTypeString(expr.Elt)
	case *ast.MapType:
		return "map[" + getFiledTypeString(expr.Key) + "]" + getFiledTypeString(expr.Value)
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.ChanType:
		return "chann " + getFiledTypeString(expr.Value)
	case *ast.Ellipsis:
		return "..." + getFiledTypeString(expr.Elt)
	default:
		panic(fmt.Sprintf("unsupported type: %T", expr))
	}
}

const setterTemplate = `
package {{.PackageName}}

import (
{{range .Imports}}
	"{{.}}"
{{end}}
)

{{range .Setters}}
func (s *{{.StructName}}) Set{{.FieldName}}(v {{.FieldType}}) {
	s.{{.FieldName}} = v
}
{{end}}
`
