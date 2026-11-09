package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	YAMLFilename   string
	snakeCaseRegex = regexp.MustCompile(`^[a-z0-9]+(_[a-z0-9]+)*$`)
	imageDomain    = "registry.bigbrother.io/"
)

func init() {
	if len(os.Args) > 1 {
		YAMLFilename = os.Args[1]
	} else {
		fmt.Printf("Usage: %s validate_me.yaml\n", os.Args[0])
		os.Exit(1)
	}
}

// Service — Ваша оригинальная структура с вложенными yaml:"..."
type Service struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string            `yaml:"name"`
		Namespace string            `yaml:"namespace"`
		Labels    map[string]string `yaml:"labels"` // Заменено на map для поддержки любых произвольных меток
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
			ReadinessProbe *struct { // Заменено на указатель, чтобы отличать отсутствие блока от пустых значений
				HTTPGet struct {
					Path string `yaml:"path"`
					Port int    `yaml:"port"`
				} `yaml:"httpGet"`
			} `yaml:"readinessProbe"`
			LivenessProbe *struct { // Заменено на указатель
				HTTPGet struct {
					Path string `yaml:"path"`
					Port int    `yaml:"port"`
				} `yaml:"httpGet"`
			} `yaml:"livenessProbe"`
			Resources struct {
				Limits struct {
					CPU    interface{} `yaml:"cpu"` // interface{} позволяет «проглотить» строку и выдать ошибку "cpu must be int"
					Memory string      `yaml:"memory"`
				} `yaml:"limits"`
				Requests struct {
					CPU    interface{} `yaml:"cpu"` // interface{}
					Memory string      `yaml:"memory"`
				} `yaml:"requests"`
			} `yaml:"resources"`
		} `yaml:"containers"`
	} `yaml:"spec"`
}

func main() {
	content, err := os.ReadFile(YAMLFilename)
	if err != nil {
		fmt.Printf("cannot read file '%s' content: %v\n", YAMLFilename, err)
		os.Exit(1)
	}

	// 1. Декодируем в AST-ноду, чтобы иметь доступ к номерам строк по путям
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Printf("cannot unmarshal file content: %v\n", err)
		os.Exit(1)
	}

	// 2. Декодируем в вашу красивую структуру для проверок бизнес-логики
	var service Service
	if err := yaml.Unmarshal(content, &service); err != nil {
		fmt.Printf("cannot unmarshal file content: %v\n", err)
		os.Exit(1)
	}

	baseName := filepath.Base(YAMLFilename)
	var errors []string

	// 1. Поля верхнего уровня
	if service.APIVersion == "" {
		errors = append(errors, fmt.Sprintf("%s:%d apiVersion is required", baseName, findLine(&root, "apiVersion")))
	} else if service.APIVersion != "v1" {
		errors = append(errors, fmt.Sprintf("%s:%d apiVersion must be v1", baseName, findLine(&root, "apiVersion")))
	}

	if service.Kind == "" {
		errors = append(errors, fmt.Sprintf("%s:%d kind is required", baseName, findLine(&root, "kind")))
	} else if service.Kind != "Pod" {
		errors = append(errors, fmt.Sprintf("%s:%d kind must be Pod", baseName, findLine(&root, "kind")))
	}

	// 2. ObjectMeta
	if service.Metadata.Name == "" {
		errors = append(errors, fmt.Sprintf("%s:%d name is required", baseName, findLine(&root, "metadata", "name")))
	}

	// 3. PodSpec & OS
	if service.Spec.Os != "" && service.Spec.Os != "linux" && service.Spec.Os != "windows" {
		errors = append(errors, fmt.Sprintf("%s:%d os has unsupported value '%s'", baseName, findLine(&root, "spec", "os"), service.Spec.Os))
	}

	// 4. Containers
	if len(service.Spec.Containers) == 0 {
		errors = append(errors, fmt.Sprintf("%s:%d containers is required", baseName, findLine(&root, "spec", "containers")))
	}

	containerNames := make(map[string]bool)
	for i, c := range service.Spec.Containers {
		cIdx := fmt.Sprintf("[%d]", i)

		// Имя контейнера
		if c.Name == "" {
			errors = append(errors, fmt.Sprintf("%s:%d name is required", baseName, findLine(&root, "spec", "containers", cIdx, "name")))
		} else {
			if !snakeCaseRegex.MatchString(c.Name) {
				errors = append(errors, fmt.Sprintf("%s:%d container name must be snake_case", baseName, findLine(&root, "spec", "containers", cIdx, "name")))
			}
			if containerNames[c.Name] {
				errors = append(errors, fmt.Sprintf("%s:%d duplicate container name", baseName, findLine(&root, "spec", "containers", cIdx, "name")))
			}
			containerNames[c.Name] = true
		}

		// Образ (Image)
		if c.Image == "" {
			errors = append(errors, fmt.Sprintf("%s:%d image is required", baseName, findLine(&root, "spec", "containers", cIdx, "image")))
		} else {
			if !strings.HasPrefix(c.Image, imageDomain) {
				errors = append(errors, fmt.Sprintf("%s:%d image registry must be %s", baseName, findLine(&root, "spec", "containers", cIdx, "image"), imageDomain))
			}
			parts := strings.Split(c.Image, ":")
			if len(parts) < 2 || parts[len(parts)-1] == "" || strings.Contains(parts[len(parts)-1], "/") {
				errors = append(errors, fmt.Sprintf("%s:%d image tag is required", baseName, findLine(&root, "spec", "containers", cIdx, "image")))
			}
		}

		// Порты
		for j, p := range c.Ports {
			pIdx := fmt.Sprintf("[%d]", j)
			if p.ContainerPort <= 0 || p.ContainerPort >= 65536 {
				errors = append(errors, fmt.Sprintf("%s:%d containerPort value out of range", baseName, findLine(&root, "spec", "containers", cIdx, "ports", pIdx, "containerPort")))
			}
			if p.Protocol != "" && p.Protocol != "TCP" && p.Protocol != "UDP" {
				errors = append(errors, fmt.Sprintf("%s:%d protocol must be TCP or UDP", baseName, findLine(&root, "spec", "containers", cIdx, "ports", pIdx, "protocol")))
			}
		}

		// Проверки Readiness и Liveness Probe
		if c.ReadinessProbe != nil {
			hg := c.ReadinessProbe.HTTPGet
			if hg.Path != "" && !strings.HasPrefix(hg.Path, "/") {
				errors = append(errors, fmt.Sprintf("%s:%d path must be absolute", baseName, findLine(&root, "spec", "containers", cIdx, "readinessProbe", "httpGet", "path")))
			}
			if hg.Port <= 0 || hg.Port >= 65536 {
				errors = append(errors, fmt.Sprintf("%s:%d port value out of range", baseName, findLine(&root, "spec", "containers", cIdx, "readinessProbe", "httpGet", "port")))
			}
		}

		if c.LivenessProbe != nil {
			hg := c.LivenessProbe.HTTPGet
			if hg.Path != "" && !strings.HasPrefix(hg.Path, "/") {
				errors = append(errors, fmt.Sprintf("%s:%d path must be absolute", baseName, findLine(&root, "spec", "containers", cIdx, "livenessProbe", "httpGet", "path")))
			}
			if hg.Port <= 0 || hg.Port >= 65536 {
				errors = append(errors, fmt.Sprintf("%s:%d port value out of range", baseName, findLine(&root, "spec", "containers", cIdx, "livenessProbe", "httpGet", "port")))
			}
		}

		// Ресурсы
		sections := []string{"requests", "limits"}
		for _, sec := range sections {
			var cpuVal interface{}
			var memVal string
			if sec == "requests" {
				cpuVal = c.Resources.Requests.CPU
				memVal = c.Resources.Requests.Memory
			} else {
				cpuVal = c.Resources.Limits.CPU
				memVal = c.Resources.Limits.Memory
			}

			if cpuVal != nil {
				if _, ok := cpuVal.(int); !ok {
					errors = append(errors, fmt.Sprintf("%s:%d cpu must be int", baseName, findLine(&root, "spec", "containers", cIdx, "resources", sec, "cpu")))
				}
			}
			if memVal != "" {
				if !strings.HasSuffix(memVal, "Gi") && !strings.HasSuffix(memVal, "Mi") && !strings.HasSuffix(memVal, "Ki") {
					errors = append(errors, fmt.Sprintf("%s:%d memory unit must be Gi, Mi, Ki", baseName, findLine(&root, "spec", "containers", cIdx, "resources", sec, "memory")))
				}
			}
		}
	}

	// Выводим все ошибки одной пачкой
	if len(errors) > 0 {
		for _, errStr := range errors {
			fmt.Println(errStr)
		}
		os.Exit(1)
	}

	fmt.Println("OK")
}

// findLine ищет номер строки в AST по цепочке ключей и индексов массивов
func findLine(root *yaml.Node, path ...string) int {
	if len(root.Content) == 0 {
		return 1
	}
	current := root.Content[0]

	for _, segment := range path {
		if current == nil {
			return 1
		}

		if strings.HasPrefix(segment, "[") && strings.HasSuffix(segment, "]") && current.Kind == yaml.SequenceNode {
			var idx int
			_, _ = fmt.Sscanf(segment, "[%d]", &idx)
			if idx >= 0 && idx < len(current.Content) {
				current = current.Content[idx]
				continue
			}
			return current.Line
		}

		if current.Kind == yaml.MappingNode {
			found := false
			for i := 0; i < len(current.Content); i += 2 {
				if current.Content[i].Value == segment {
					current = current.Content[i+1]
					found = true
					break
				}
			}
			if !found {
				return current.Line
			}
		}
	}

	if current != nil {
		return current.Line
	}
	return 1
}
