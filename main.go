package main

import (
	"fmt"
	"os"
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

// Service представляет структуру Pod для валидации
type Service struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name      string            `yaml:"name"`
		Namespace string            `yaml:"namespace"`
		Labels    map[string]string `yaml:"labels"`
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
			ReadinessProbe *struct {
				HTTPGet struct {
					Path string `yaml:"path"`
					Port int    `yaml:"port"`
				} `yaml:"httpGet"`
			} `yaml:"readinessProbe"`
			LivenessProbe *struct {
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
	content, err := os.ReadFile(YAMLFilename)
	if err != nil {
		fmt.Printf("cannot read file '%s' content: %v\n", YAMLFilename, err)
		os.Exit(1)
	}

	// Критическая ошибка 1: Невалидный синтаксис YAML структуры
	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Printf("Критическая ошибка: невозможно разобрать синтаксис YAML: %v\n", err)
		os.Exit(1)
	}

	// Критическая ошибка 2: Отсутствие обязательных полей верхнего уровня
	if err := validateTopLevelFields(&root); err != nil {
		fmt.Printf("Критическая ошибка структуры: %v\n", err)
		os.Exit(1)
	}

	var service Service
	if err := yaml.Unmarshal(content, &service); err != nil {
		fmt.Printf("Критическая ошибка соответствия типов данных: %v\n", err)
		os.Exit(1)
	}

	// Непрерывная валидация бизнес-логики: собираем срез всех ошибок
	validationErrors := validateService(&service)
	if len(validationErrors) > 0 {
		fmt.Printf("Валидация провалена (%d нарушений ТЗ):\n", len(validationErrors))
		for _, errStr := range validationErrors {
			fmt.Printf("  - %s\n", errStr)
		}
		os.Exit(1)
	}

	fmt.Println("Валидация успешно пройдена без замечаний!")
}

func validateTopLevelFields(root *yaml.Node) error {
	// root.Content содержит список документов в YAML файле
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.DocumentNode {
		return fmt.Errorf("неверный формат YAML документа")
	}

	docNode := root.Content[0]
	// docNode.Content[0] содержит корневой MappingNode для структуры Pod
	if len(docNode.Content) == 0 || docNode.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("корневой элемент должен быть объектом (map)")
	}

	docMapping := docNode.Content[0]
	foundFields := make(map[string]bool)
	
	// В MappingNode элементы идут парами: Ключ (i) -> Значение (i+1)
	for i := 0; i < len(docMapping.Content); i += 2 {
		foundFields[docMapping.Content[i].Value] = true
	}

	required := []string{"apiVersion", "kind", "metadata", "spec"}
	for _, field := range required {
		if !foundFields[field] {
			return fmt.Errorf("отсутствует обязательное поле верхнего уровня '%s'", field)
		}
	}
	return nil
}

func validateService(s *Service) []string {
	var errs []string

	if s.APIVersion != "v1" {
		errs = append(errs, fmt.Sprintf("apiVersion должен быть 'v1', получено: '%s'", s.APIVersion))
	}
	if s.Kind != "Pod" {
		errs = append(errs, fmt.Sprintf("kind должен быть 'Pod', получено: '%s'", s.Kind))
	}
	if strings.TrimSpace(s.Metadata.Name) == "" {
		errs = append(errs, "metadata.name является обязательным и не может быть пустым")
	}
	if s.Spec.Os != "" && s.Spec.Os != "linux" && s.Spec.Os != "windows" {
		errs = append(errs, fmt.Sprintf("spec.os должен быть 'linux' или 'windows', получено: '%s'", s.Spec.Os))
	}
	if len(s.Spec.Containers) == 0 {
		errs = append(errs, "список spec.containers не должен быть пустым")
		return errs // Дальнейшая валидация контейнеров бессмысленна
	}

	containerNames := make(map[string]bool)
	for i, c := range s.Spec.Containers {
		if strings.TrimSpace(c.Name) == "" {
			errs = append(errs, fmt.Sprintf("spec.containers[%d].name обязателен", i))
		} else {
			if !snakeCaseRegex.MatchString(c.Name) {
				errs = append(errs, fmt.Sprintf("spec.containers[%d].name ('%s') должно быть в snake_case", i, c.Name))
			}
			if containerNames[c.Name] {
				errs = append(errs, fmt.Sprintf("имя контейнера '%s' дублируется в поде", c.Name))
			}
			containerNames[c.Name] = true
		}

		if strings.TrimSpace(c.Image) == "" {
			errs = append(errs, fmt.Sprintf("spec.containers[%d].image обязателен", i))
		} else {
			if !strings.HasPrefix(c.Image, imageDomain) {
				errs = append(errs, fmt.Sprintf("spec.containers[%d].image должен начинаться с '%s'", i, imageDomain))
			}
			parts := strings.Split(c.Image, ":")
			if len(parts) < 2 || parts[len(parts)-1] == "" || strings.Contains(parts[len(parts)-1], "/") {
				errs = append(errs, fmt.Sprintf("spec.containers[%d].image ('%s') должен содержать тег версии", i, c.Image))
			}
		}

		for j, p := range c.Ports {
			if p.ContainerPort <= 0 || p.ContainerPort >= 65536 {
				errs = append(errs, fmt.Sprintf("spec.containers[%d].ports[%d].containerPort должен быть в пределах 0 < x < 65536", i, j))
			}
			if p.Protocol != "" && p.Protocol != "TCP" && p.Protocol != "UDP" {
				errs = append(errs, fmt.Sprintf("spec.containers[%d].ports[%d].protocol должен быть TCP или UDP", i, j))
			}
		}

		if c.ReadinessProbe != nil {
			if err := validateProbePathAndPort(c.ReadinessProbe.HTTPGet.Path, c.ReadinessProbe.HTTPGet.Port, fmt.Sprintf("spec.containers[%d].readinessProbe", i)); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if c.LivenessProbe != nil {
			if err := validateProbePathAndPort(c.LivenessProbe.HTTPGet.Path, c.LivenessProbe.HTTPGet.Port, fmt.Sprintf("spec.containers[%d].livenessProbe", i)); err != nil {
				errs = append(errs, err.Error())
			}
		}
		if err := validateMemory(c.Resources.Limits.Memory, fmt.Sprintf("spec.containers[%d].resources.limits.memory", i)); err != nil {
			errs = append(errs, err.Error())
		}
		if err := validateMemory(c.Resources.Requests.Memory, fmt.Sprintf("spec.containers[%d].resources.requests.memory", i)); err != nil {
			errs = append(errs, err.Error())
		}
	}
	return errs
}

func validateProbePathAndPort(path string, port int, context string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%s.httpGet.path должен быть абсолютным (начинаться с '/')", context)
	}
	if port <= 0 || port >= 65536 {
		return fmt.Errorf("%s.httpGet.port должен быть в пределах 0 < x < 65536", context)
	}
	return nil
}

func validateMemory(mem string, context string) error {
	if mem == "" {
		return nil
	}
	if !strings.HasSuffix(mem, "Gi") && !strings.HasSuffix(mem, "Mi") && !strings.HasSuffix(mem, "Ki") {
		return fmt.Errorf("%s должен использовать единицы степени двойки (Gi, Mi, Ki)", context)
	}
	return nil
}
