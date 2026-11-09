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
		YAMLFilename = os.Args
	} else {
		fmt.Printf("Usage: %s validate_me.yaml\n", os.Args)
		os.Exit(1)
	}
}

// Service — структура с вложенными yaml:"..."
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
					CPU    interface{} `yaml:"cpu"`
					Memory string      `yaml:"memory"`
				} `yaml:"limits"`
				Requests struct {
					CPU    interface{} `yaml:"cpu"`
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

	var root yaml.Node
	var service Service

	if err := yaml.Unmarshal(content, &root); err != nil || yaml.Unmarshal(content, &service) != nil {
		fmt.Println("cannot unmarshal file content")
		os.Exit(1)
	}

	v := NewValidator(&root, filepath.Base(YAMLFilename))

	// 1. Поля верхнего уровня
	if service.APIVersion == "" {
		v.AddErr("apiVersion is required", "apiVersion")
	} else if service.APIVersion != "v1" {
		v.AddErr("apiVersion must be v1", "apiVersion")
	}

	if service.Kind == "" {
		v.AddErr("kind is required", "kind")
	} else if service.Kind != "Pod" {
		v.AddErr("kind must be Pod", "kind")
	}

	// 2. ObjectMeta
	if service.Metadata.Name == "" {
		v.AddErr("name is required", "metadata", "name")
	}

	// 3. PodSpec & OS
	if service.Spec.Os != "" && service.Spec.Os != "linux" && service.Spec.Os != "windows" {
		v.AddErr(fmt.Sprintf("os has unsupported value '%s'", service.Spec.Os), "spec", "os")
	}

	// 4. Containers
	if len(service.Spec.Containers) == 0 {
		v.AddErr("containers is required", "spec", "containers")
	}

	containerNames := make(map[string]bool)
	for i, c := range service.Spec.Containers {
		cIdx := fmt.Sprintf("[%d]", i)

		// Имя контейнера
		if c.Name == "" {
			v.AddErr("name is required", "spec", "containers", cIdx, "name")
		} else {
			if !snakeCaseRegex.MatchString(c.Name) {
				v.AddErr("container name must be snake_case", "spec", "containers", cIdx, "name")
			}
			if containerNames[c.Name] {
				v.AddErr("duplicate container name", "spec", "containers", cIdx, "name")
			}
			containerNames[c.Name] = true
		}

		// Образ (Image)
		if c.Image == "" {
			v.AddErr("image is required", "spec", "containers", cIdx, "image")
		} else {
			if !strings.HasPrefix(c.Image, imageDomain) {
				v.AddErr(fmt.Sprintf("image registry must be %s", imageDomain), "spec", "containers", cIdx, "image")
			}
			parts := strings.Split(c.Image, ":")
			if len(parts) < 2 || parts[len(parts)-1] == "" || strings.Contains(parts[len(parts)-1], "/") {
				v.AddErr("image tag is required", "spec", "containers", cIdx, "image")
			}
		}

		// Порты
		for j, p := range c.Ports {
			pIdx := fmt.Sprintf("[%d]", j)
			if p.ContainerPort <= 0 || p.ContainerPort >= 65536 {
				v.AddErr("containerPort value out of range", "spec", "containers", cIdx, "ports", pIdx, "containerPort")
			}
			if p.Protocol != "" && p.Protocol != "TCP" && p.Protocol != "UDP" {
				v.AddErr("protocol must be TCP or UDP", "spec", "containers", cIdx, "ports", pIdx, "protocol")
			}
		}

		// Валидация Readiness и Liveness Probe без хаков рефлексии
		if c.ReadinessProbe != nil {
			validateProbe(v, cIdx, "readinessProbe", c.ReadinessProbe.HTTPGet.Path, c.ReadinessProbe.HTTPGet.Port)
		}
		if c.LivenessProbe != nil {
			validateProbe(v, cIdx, "livenessProbe", c.LivenessProbe.HTTPGet.Path, c.LivenessProbe.HTTPGet.Port)
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
					v.AddErr("cpu must be int", "spec", "containers", cIdx, "resources", sec, "cpu")
				}
			}
			if memVal != "" {
				if !strings.HasSuffix(memVal, "Gi") && !strings.HasSuffix(memVal, "Mi") && !strings.HasSuffix(memVal, "Ki") {
					v.AddErr("memory unit must be Gi, Mi, Ki", "spec", "containers", cIdx, "resources", sec, "memory")
				}
			}
		}
	}

	if v.HasErrors() {
		v.PrintAndExit()
	}

	fmt.Println("OK")
}

// Чистая изолированная функция для проверки параметров Probe
func validateProbe(v *Validator, cIdx, probeName, path string, port int) {
	if path != "" && !strings.HasPrefix(path, "/") {
		v.AddErr("path must be absolute", "spec", "containers", cIdx, probeName, "httpGet", "path")
	}
	if port <= 0 || port >= 65536 {
		v.AddErr("port value out of range", "spec", "containers", cIdx, probeName, "httpGet", "port")
	}
}

// --- Умный хелпер-валидатор ---

type Validator struct {
	root     *yaml.Node
	baseName string
	errors   []string
}

func NewValidator(root *yaml.Node, baseName string) *Validator {
	return &Validator{root: root, baseName: baseName}
}

func (v *Validator) AddErr(msg string, path ...string) {
	line := 1
	if len(v.root.Content) > 0 {
		current := v.root.Content
		for _, segment := range path {
			if current == nil {
				break
			}
			if strings.HasPrefix(segment, "[") && strings.HasSuffix(segment, "]") && current.Kind == yaml.SequenceNode {
				var idx int
				_, _ = fmt.Sscanf(segment, "[%d]", &idx)
				if idx >= 0 && idx < len(current.Content) {
					current = current.Content[idx]
					continue
				}
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
					break
				}
			}
		}
		if current != nil {
			line = current.Line
		}
	}
	v.errors = append(v.errors, fmt.Sprintf("%s:%d %s", v.baseName, line, msg))
}

func (v *Validator) HasErrors() bool {
	return len(v.errors) > 0
}

func (v *Validator) PrintAndExit() {
	for _, errStr := range v.errors {
		fmt.Println(errStr)
	}
	os.Exit(1)
}
