package types

type ProfileSpec struct {
	Name           string               `yaml:"name"           json:"name"`
	Infrastructure InfrastructureConfig `yaml:"infrastructure" json:"infrastructure"`
	Projects       []ProjectConfig      `yaml:"projects"       json:"projects"`
	Agent          AgentConfig          `yaml:"agent"          json:"agent"`
}

type InfrastructureConfig struct {
	Provider string   `yaml:"provider" json:"provider"`
	Image    string   `yaml:"image"    json:"image"`
	Tooling  []string `yaml:"tooling"  json:"tooling"`
}

type ProjectConfig struct {
	Repo string `yaml:"repo" json:"repo"`
	Path string `yaml:"path" json:"path"`
}

type AgentConfig struct {
	Provider string   `yaml:"provider" json:"provider"`
	Skills   []string `yaml:"skills"   json:"skills"`
}
