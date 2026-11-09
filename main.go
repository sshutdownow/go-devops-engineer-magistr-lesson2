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

// --- Красивые декларативные структуры с типизированными нодами ---

type Pod struct {
	APIVersion yaml.Node  `yaml:"apiVersion"`
	Kind       yaml.Node  `yaml:"kind"`
	Metadata   Metadata   `yaml:"metadata"`
	Spec       PodSpec    `yaml:"spec"`
}

type Metadata struct {
	Name      yaml.Node         `yaml:"name"`
	Namespace yaml.Node         `yaml:"namespace"`
	Labels    map[string]string `yaml:"labels"`
}

type PodSpec struct {
	OS         yaml.Node   `yaml:"os"` // Может содержать строку или мапу
	Containers []Container `yaml:"containers"`
}

type Container struct {
	Name           yaml.Node            `yaml:"name"`
	Image          yaml.Node            `yaml:"image"`
	Ports          []ContainerPort      `yaml:"ports"`
	ReadinessProbe *Probe               `yaml:"readinessProbe"`
	LivenessProbe  *Probe               `yaml:"livenessProbe"`
	Resources      ResourceRequirements `yaml:"resources"`
}

type ContainerPort struct {
	ContainerPort yaml.Node `yaml:"containerPort"`
	Protocol      yaml.Node `yaml:"protocol"`
}

type Probe struct {
	HTTPGet HTTPGetAction `yaml:"httpGet"`
}

type HTTPGetAction struct {
	Path yaml.Node `yaml:"path"`
	Port yaml.Node `yaml:"port"`
}

type ResourceRequirements struct {
	Requests ResourceMap `yaml:"requests"`
	Limits   ResourceMap `yaml:"limits"`
}

type ResourceMap struct {
	CPU    yaml.Node `yaml:"cpu"`
	Memory yaml.Node `yaml:"memory"`
}

func init() {
	if len(os.Args) > 1 {
		YAMLFilename = os.Args[1]
	} else {
		fmt.Printf("Usage: %s validate_me.yaml\n", os.Args[0])
		os.Exit(1)
	}
}

func main() {
	content, err := os.ReadFile(YAMLFilename)
	if err != nil {
		fmt.Printf("cannot read file '%s' content: %v\n", YAMLFilename, err)
		os.Exit(1)
	}

	// Мапим сразу в красивую структуру Pod
	var pod Pod
	if err := yaml.Unmarshal(content, &pod); err != nil {
		fmt.Printf("cannot unmarshal file content: %v\n", err)
		os.Exit(1)
	}

	baseName := filepath.Base(YAMLFilename)
	var errors []string

	// 1. Поля верхнего уровня
	if pod.APIVersion.Value == "" {
		errors = append(errors, fmt.Sprintf("%s:1 apiVersion is required", baseName))
	} else if pod.APIVersion.Value != "v1" {
		errors = append(errors, fmt.Sprintf("%s:%d apiVersion must be v1", baseName, pod.APIVersion.Line))
	}

	if pod.Kind.Value == "" {
		errors = append(errors, fmt.Sprintf("%s:1 kind is required", baseName))
	} else if pod.Kind.Value != "Pod" {
		errors = append(errors, fmt.Sprintf("%s:%d kind must be Pod", baseName, pod.Kind.Line))
	}

	// 2. ObjectMeta
	if pod.Metadata.Name.Value == "" {
		errors = append(errors, fmt.Sprintf("%s:1 name is required", baseName))
	}

	// 3. PodSpec & OS
	if pod.Spec.OS.Value != "" { // если os задана строкой
		if pod.Spec.OS.Value != "linux" && pod.Spec.OS.Value != "windows" {
			errors = append(errors, fmt.Sprintf("%s:%d os has unsupported value '%s'", baseName, pod.Spec.OS.Line, pod.Spec.OS.Value))
		}
	} else if pod.Spec.OS.Kind == yaml.MappingNode { // если задана объектом {name: ...}
		var osObj struct { Name yaml.Node `yaml:"name"` }
		_ = pod.Spec.OS.Decode(&osObj)
		if osObj.Name.Value != "linux" && osObj.Name.Value != "windows" {
			errors = append(errors, fmt.Sprintf("%s:%d os has unsupported value '%s'", baseName, osObj.Name.Line, osObj.Name.Value))
		}
	}

	// 4. Containers
	if len(pod.Spec.Containers) == 0 {
		errors = append(errors, fmt.Sprintf("%s:1 containers is required", baseName))
	}

	containerNames := make(map[string]bool)
	for _, c := range pod.Spec.Containers {
		// Имя контейнера
		if c.Name.Value == "" {
			errors = append(errors, fmt.Sprintf("%s:%d name is required", baseName, c.Name.Line))
		} else {
			if !snakeCaseRegex.MatchString(c.Name.Value) {
				errors = append(errors, fmt.Sprintf("%s:%d container name must be snake_case", baseName, c.Name.Line))
			}
			if containerNames[c.Name.Value] {
				errors = append(errors, fmt.Sprintf("%s:%d duplicate container name", baseName, c.Name.Line))
			}
			containerNames[c.Name.Value] = true
		}

		// Образ (Image)
		if c.Image.Value == "" {
			errors = append(errors, fmt.Sprintf("%s:1 image is required", baseName))
		} else {
			if !strings.HasPrefix(c.Image.Value, imageDomain) {
				errors = append(errors, fmt.Sprintf("%s:%d image registry must be %s", baseName, c.Image.Line, imageDomain))
			}
			parts := strings.Split(c.Image.Value, ":")
			if len(parts) < 2 || parts[len(parts)-1] == "" || strings.Contains(parts[len(parts)-1], "/") {
				errors = append(errors, fmt.Sprintf("%s:%d image tag is required", baseName, c.Image.Line))
			}
		}

		// Порты
		for _, p := range c.Ports {
			if p.ContainerPort.Value != "" {
				var portVal int
				_ = p.ContainerPort.Decode(&portVal)
				if portVal <= 0 || portVal >= 65536 {
					errors = append(errors, fmt.Sprintf("%s:%d containerPort value out of range", baseName, p.ContainerPort.Line))
				}
			}
			if p.Protocol.Value != "" && p.Protocol.Value != "TCP" && p.Protocol.Value != "UDP" {
				errors = append(errors, fmt.Sprintf("%s:%d protocol must be TCP or UDP", baseName, p.Protocol.Line))
			}
		}

		// Пробы (Probes)
		probes := map[string]*Probe{"readinessProbe": c.ReadinessProbe, "livenessProbe": c.LivenessProbe}
		for _, pr := range probes {
			if pr != nil {
				hg := pr.HTTPGet
				if hg.Path.Value != "" && !strings.HasPrefix(hg.Path.Value, "/") {
					errors = append(errors, fmt.Sprintf("%s:%d path must be absolute", baseName, hg.Path.Line))
				}
				if hg.Port.Value != "" {
					var portVal int
					_ = hg.Port.Decode(&portVal)
					if portVal <= 0 || portVal >= 65536 {
						errors = append(errors, fmt.Sprintf("%s:%d port value out of range", baseName, hg.Port.Line))
					}
				}
			}
		}

		// Ресурсы
		resources := []ResourceMap{c.Resources.Requests, c.Resources.Limits}
		for _, res := range resources {
			if res.CPU.Value != "" && res.CPU.ShortTag() != "!!int" {
				errors = append(errors, fmt.Sprintf("%s:%d cpu must be int", baseName, res.CPU.Line))
			}
			if res.Memory.Value != "" {
				v := res.Memory.Value
				if !strings.HasSuffix(v, "Gi") && !strings.HasSuffix(v, "Mi") && !strings.HasSuffix(v, "Ki") {
					errors = append(errors, fmt.Sprintf("%s:%d memory unit must be Gi, Mi, Ki", baseName, res.Memory.Line))
				}
			}
		}
	}

	// Вывод ошибок валидации
	if len(errors) > 0 {
		for _, errStr := range errors {
			fmt.Println(errStr)
		}
		os.Exit(1)
	}

	fmt.Println("OK")
}
