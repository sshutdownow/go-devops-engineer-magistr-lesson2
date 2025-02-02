package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var YAMLFilename = flag.String("YAMLFilename", "", "YAMLFilename to validate")

func init() {
	flag.Usage = func() {
		fmt.Printf("Usage: %s YAMLFilenamet='validate_me.yaml'\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(0)
	}
	flag.Parse()
}

func main() {
	// получим содержимое файла
	content, err := os.ReadFile(*YAMLFilename)
	if err != nil {
		fmt.Errorf("cannot read file content: %w", err)
	}

	// преобразуем байты из файла в промежуточное синтаксическое представление
	// с использованием типа `yaml.Node` в качестве целевого объекта
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Errorf("cannot unmarshal file content: %w", err)
	}

	// пройдёмся по элементам верхнего уровня и
	// выведем их базовую информацию на экран
	for _, doc := range root.Content {
		// поле `.Content` содержит вложенные дочерние элементы
		for _, node := range doc.Content {
			fmt.Printf("line: %d, value: %s\n", node.Line, node.Value)
		}
	}
}
