package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

var YAMLFilename string

func init() {
	if len(os.Args) > 1 {
		YAMLFilename = os.Args[1]
	} else {
		fmt.Printf("Usage: %s validate_me.yaml\n", os.Args[0])
		os.Exit(0)
	}
}

type Service struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string `yaml:"name"`
		Namespace string `yaml:"namespace"`
		Labels    struct {
			Dc    string `yaml:"dc"`
			Group string `yaml:"group"`
		} `yaml:"labels"`
	} `yaml:"metadata"`
	Spec struct {
		Os         string `yaml:"os"`
		Containers []struct {
			Name  string `yaml:"name"`
			Image string `yaml:"image"`
			Ports []struct {
				ContainerPort int    `yaml:"containerPort"`
				Protocol      string `yaml:"protocol"`
			} `yaml:"ports"`
			ReadinessProbe struct {
				HTTPGet struct {
					Path string `yaml:"path"`
					Port int    `yaml:"port"`
				} `yaml:"httpGet"`
			} `yaml:"readinessProbe"`
			LivenessProbe struct {
				HTTPGet struct {
					Path string `yaml:"path"`
					Port int    `yaml:"port"`
				} `yaml:"httpGet"`
			} `yaml:"livenessProbe"`
			Resources struct {
				Limits struct {
					CPU    int    `yaml:"cpu"`
					Memory string `yaml:"memory"`
				} `yaml:"limits"`
				Requests struct {
					CPU    int    `yaml:"cpu"`
					Memory string `yaml:"memory"`
				} `yaml:"requests"`
			} `yaml:"resources"`
		} `yaml:"containers"`
	} `yaml:"spec"`
}

func main() {
	// получим содержимое файла
	content, err := os.ReadFile(YAMLFilename)
	if err != nil {
		fmt.Printf("cannot read file '%s' content: %v", YAMLFilename, err)
		return
	}
	//defer file.Close()

	// преобразуем байты из файла в промежуточное синтаксическое представление
	// с использованием типа `yaml.Node` в качестве целевого объекта
	var root yaml.Node
	var service Service
	if err := yaml.Unmarshal(content, &service /*root*/); err != nil {
		fmt.Printf("cannot unmarshal file content: %v", err)
		return
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
