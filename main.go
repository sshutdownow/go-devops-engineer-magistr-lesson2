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

func main() {
	content, err := os.ReadFile(YAMLFilename)
	if err != nil {
		fmt.Printf("cannot read file '%s' content: %v\n", YAMLFilename, err)
		os.Exit(1)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(content, &root); err != nil {
		fmt.Printf("cannot unmarshal file content: %v\n", err)
		os.Exit(1)
	}

	if len(root.Content) == 0 {
		fmt.Println("empty yaml document")
		os.Exit(1)
	}

	baseName := filepath.Base(YAMLFilename)
	var errors []string

	podNode := root.Content
	if podNode.Kind != yaml.MappingNode {
		fmt.Println("root element must be a map")
		os.Exit(1)
	}

	fields := getMapFields(podNode)

	// 1. Поля верхнего уровня
	if node, ok := fields["apiVersion"]; !ok {
		errors = append(errors, fmt.Sprintf("%s:%d apiVersion is required", baseName, podNode.Line))
	} else if node.Value != "v1" {
		errors = append(errors, fmt.Sprintf("%s:%d apiVersion must be v1", baseName, node.Line))
	}

	if node, ok := fields["kind"]; !ok {
		errors = append(errors, fmt.Sprintf("%s:%d kind is required", baseName, podNode.Line))
	} else if node.Value != "Pod" {
		errors = append(errors, fmt.Sprintf("%s:%d kind must be Pod", baseName, node.Line))
	}

	// 2. ObjectMeta
	var metadataFields map[string]*yaml.Node
	if node, ok := fields["metadata"]; !ok {
		errors = append(errors, fmt.Sprintf("%s:%d metadata is required", baseName, podNode.Line))
	} else if node.Kind == yaml.MappingNode {
		metadataFields = getMapFields(node)
		if nameNode, ok := metadataFields["name"]; !ok || strings.TrimSpace(nameNode.Value) == "" {
			line := node.Line
			if ok {
				line = nameNode.Line
			}
			errors = append(errors, fmt.Sprintf("%s:%d name is required", baseName, line))
		}
	}

	// 3. PodSpec
	if node, ok := fields["spec"]; !ok {
		errors = append(errors, fmt.Sprintf("%s:%d spec is required", baseName, podNode.Line))
	} else if node.Kind == yaml.MappingNode {
		specFields := getMapFields(node)

		// Валидация OS
		if osNode, ok := specFields["os"]; ok {
			if osNode.Kind == yaml.ScalarNode {
				if osNode.Value != "linux" && osNode.Value != "windows" {
					errors = append(errors, fmt.Sprintf("%s:%d os has unsupported value '%s'", baseName, osNode.Line, osNode.Value))
				}
			} else if osNode.Kind == yaml.MappingNode {
				osFields := getMapFields(osNode)
				if nameNode, ok := osFields["name"]; !ok || (nameNode.Value != "linux" && nameNode.Value != "windows") {
					line := osNode.Line
					val := ""
					if ok {
						line = nameNode.Line
						val = nameNode.Value
					}
					errors = append(errors, fmt.Sprintf("%s:%d os has unsupported value '%s'", baseName, line, val))
				}
			}
		}

		// Валидация Containers
		if containersNode, ok := specFields["containers"]; !ok || len(containersNode.Content) == 0 {
			errors = append(errors, fmt.Sprintf("%s:%d containers is required", baseName, node.Line))
		} else if containersNode.Kind == yaml.SequenceNode {
			containerNames := make(map[string]bool)

			for _, containerNode := range containersNode.Content {
				if containerNode.Kind != yaml.MappingNode {
					continue
				}
				cFields := getMapFields(containerNode)

				nameNode, hasName := cFields["name"]
				if !hasName || strings.TrimSpace(nameNode.Value) == "" {
					line := containerNode.Line
					if hasName {
						line = nameNode.Line
					}
					errors = append(errors, fmt.Sprintf("%s:%d name is required", baseName, line))
				} else {
					if !snakeCaseRegex.MatchString(nameNode.Value) {
						errors = append(errors, fmt.Sprintf("%s:%d container name must be snake_case", baseName, nameNode.Line))
					}
					if containerNames[nameNode.Value] {
						errors = append(errors, fmt.Sprintf("%s:%d duplicate container name", baseName, nameNode.Line))
					}
					containerNames[nameNode.Value] = true
				}

				imageNode, hasImage := cFields["image"]
				if !hasImage || strings.TrimSpace(imageNode.Value) == "" {
					errors = append(errors, fmt.Sprintf("%s:%d image is required", baseName, containerNode.Line))
				} else {
					if !strings.HasPrefix(imageNode.Value, imageDomain) {
						errors = append(errors, fmt.Sprintf("%s:%d image registry must be %s", baseName, imageNode.Line, imageDomain))
					}
					parts := strings.Split(imageNode.Value, ":")
					if len(parts) < 2 || parts[len(parts)-1] == "" || strings.Contains(parts[len(parts)-1], "/") {
						errors = append(errors, fmt.Sprintf("%s:%d image tag is required", baseName, imageNode.Line))
					}
				}

				// Порты
				if portsNode, ok := cFields["ports"]; ok && portsNode.Kind == yaml.SequenceNode {
					for _, portNode := range portsNode.Content {
						if portNode.Kind != yaml.MappingNode {
							continue
						}
						pFields := getMapFields(portNode)
						if cpNode, ok := pFields["containerPort"]; ok {
							var p int
							if err := cpNode.Decode(&p); err == nil {
								if p <= 0 || p >= 65536 {
									errors = append(errors, fmt.Sprintf("%s:%d port out of range", baseName, cpNode.Line))
								}
							}
						}
						if protoNode, ok := pFields["protocol"]; ok {
							if protoNode.Value != "TCP" && protoNode.Value != "UDP" {
								errors = append(errors, fmt.Sprintf("%s:%d protocol must be TCP or UDP", baseName, protoNode.Line))
							}
						}
					}
				}

				// Проверки Readiness и Liveness Probe
				for _, probeName := range []string{"readinessProbe", "livenessProbe"} {
					if probeNode, ok := cFields[probeName]; ok && probeNode.Kind == yaml.MappingNode {
						prFields := getMapFields(probeNode)
						if hgNode, ok := prFields["httpGet"]; ok && hgNode.Kind == yaml.MappingNode {
							hgFields := getMapFields(hgNode)
							if pathNode, ok := hgFields["path"]; ok {
								if !strings.HasPrefix(pathNode.Value, "/") {
									errors = append(errors, fmt.Sprintf("%s:%d path must be absolute", baseName, pathNode.Line))
								}
							}
							if pNode, ok := hgFields["port"]; ok {
								var p int
								if err := pNode.Decode(&p); err == nil {
									if p <= 0 || p >= 65536 {
										errors = append(errors, fmt.Sprintf("%s:%d port out of range", baseName, pNode.Line))
									}
								}
							}
						}
					}
				}

				// Ресурсы
				if resNode, ok := cFields["resources"]; !ok {
					errors = append(errors, fmt.Sprintf("%s:%d resources is required", baseName, containerNode.Line))
				} else if resNode.Kind == yaml.MappingNode {
					resFields := getMapFields(resNode)
					for _, section := range []string{"requests", "limits"} {
						if secNode, ok := resFields[section]; ok && secNode.Kind == yaml.MappingNode {
							secFields := getMapFields(secNode)
							if cpuNode, ok := secFields["cpu"]; ok {
								if cpuNode.ShortTag() != "!!int" {
									errors = append(errors, fmt.Sprintf("%s:%d cpu must be int", baseName, cpuNode.Line))
								}
							}
							if memNode, ok := secFields["memory"]; ok {
								v := memNode.Value
								if !strings.HasSuffix(v, "Gi") && !strings.HasSuffix(v, "Mi") && !strings.HasSuffix(v, "Ki") {
									errors = append(errors, fmt.Sprintf("%s:%d memory unit must be Gi, Mi, Ki", baseName, memNode.Line))
								}
							}
						}
					}
				}
			}
		}
	}

	if len(errors) > 0 {
		for _, errStr := range errors {
			fmt.Println(errStr)
		}
		os.Exit(1)
	}

	fmt.Println("OK")
}

func getMapFields(node *yaml.Node) map[string]*yaml.Node {
	fields := make(map[string]*yaml.Node)
	if node.Kind != yaml.MappingNode {
		return fields
	}
	for i := 0; i < len(node.Content); i += 2 {
		fields[node.Content[i].Value] = node.Content[i+1]
	}
	return fields
}
