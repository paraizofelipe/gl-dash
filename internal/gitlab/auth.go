package gitlab

import (
	"os"
	"path/filepath"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type AuthConfig struct {
	Host        string
	Token       string
	APIProtocol string
}

func LoadAuthConfig() (AuthConfig, error) {
	host := "gitlab.com"
	if v := os.Getenv("GITLAB_HOST"); v != "" {
		host = v
	}

	cfg, _ := loadFromFile(defaultConfigPath(), host)
	cfg.Host = host
	if cfg.APIProtocol == "" {
		cfg.APIProtocol = "https"
	}
	if tok := os.Getenv("GITLAB_TOKEN"); tok != "" {
		cfg.Token = tok
	} else if cfg.Token == "" {
		cfg.Token = os.Getenv("CI_JOB_TOKEN")
	}
	return cfg, nil
}

func defaultConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "glab-cli", "config.yml")
}

func loadFromFile(path, host string) (AuthConfig, error) {
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return AuthConfig{}, err
	}
	return AuthConfig{
		Token:       k.String("hosts." + host + ".token"),
		APIProtocol: k.String("hosts." + host + ".api_protocol"),
	}, nil
}
