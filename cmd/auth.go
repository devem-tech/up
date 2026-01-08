package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"

	"github.com/moby/moby/api/pkg/authconfig"
	"github.com/moby/moby/api/types/registry"
)

type dockerAuthIndex map[string]registry.AuthConfig

type dockerConfigFile struct {
	Auths map[string]struct {
		Auth string `json:"auth"` // base64(user:pass)
	} `json:"auths"`
}

func loadDockerConfigAuths(path string) (dockerAuthIndex, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg dockerConfigFile
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}

	out := dockerAuthIndex{}
	for server, entry := range cfg.Auths {
		if entry.Auth == "" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(entry.Auth)
		if err != nil {
			continue
		}
		user, pass, _ := strings.Cut(string(raw), ":")
		out[normalizeRegistryKey(server)] = registry.AuthConfig{
			Username:      user,
			Password:      pass,
			ServerAddress: server,
		}
	}
	return out, nil
}

func (idx dockerAuthIndex) registryAuthForImageRef(imageRef string) (string, bool) {
	reg := registryFromImageRef(imageRef)

	// docker config часто хранит ключ "https://index.docker.io/v1/" для Docker Hub
	candidates := []string{
		normalizeRegistryKey(reg),
		normalizeRegistryKey("https://" + reg),
		normalizeRegistryKey("https://" + reg + "/v1/"),
		normalizeRegistryKey("https://index.docker.io/v1/"), // частый случай
	}

	for _, k := range candidates {
		if ac, ok := idx[k]; ok {
			enc, err := authconfig.Encode(ac)
			if err != nil {
				return "", false
			}
			return enc, true
		}
	}
	return "", false
}

func registryFromImageRef(ref string) string {
	// Правило как в Docker:
	// если первый сегмент содержит '.' или ':' или равен 'localhost' — это registry host.
	// иначе Docker Hub.
	first := ref
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		first = ref[:i]
	}
	if strings.Contains(first, ".") || strings.Contains(first, ":") || first == "localhost" {
		return first
	}
	return "index.docker.io"
}

func normalizeRegistryKey(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}
