package main

import (
	"strings"

	"github.com/pkg/errors"
)

// Configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration, as well as values computed from the configuration.
type Configuration struct {
	GitLabURL     string `json:"GitLabURL"`
	GitLabToken   string `json:"GitLabToken"`
	WebhookSecret string `json:"WebhookSecret"`
	FabrixAPIURL  string `json:"FabrixAPIURL"`
	FabrixAPIKey  string `json:"FabrixAPIKey"`
}

// IsValid checks if the configuration is valid.
func (c *Configuration) IsValid() error {
	if c.GitLabURL == "" {
		return errors.New("GitLab URL을 입력해주세요")
	}
	if !strings.HasPrefix(c.GitLabURL, "http://") && !strings.HasPrefix(c.GitLabURL, "https://") {
		return errors.New("GitLab URL은 http:// 또는 https://로 시작해야 합니다")
	}
	if c.GitLabToken == "" {
		return errors.New("GitLab Personal Access Token을 입력해주세요")
	}
	return nil
}

func (p *Plugin) getConfiguration() *Configuration {
	p.configurationLock.RLock()
	defer p.configurationLock.RUnlock()

	if p.configuration == nil {
		return &Configuration{}
	}
	return p.configuration
}

func (p *Plugin) setConfiguration(configuration *Configuration) {
	p.configurationLock.Lock()
	defer p.configurationLock.Unlock()

	if configuration != nil && p.configuration == configuration {
		panic("setConfiguration called with the existing configuration")
	}

	p.configuration = configuration
}

// OnConfigurationChange is invoked when configuration changes may have been made.
func (p *Plugin) OnConfigurationChange() error {
	var configuration = new(Configuration)

	if err := p.API.LoadPluginConfiguration(configuration); err != nil {
		return errors.Wrap(err, "failed to load plugin configuration")
	}

	configuration.GitLabURL = strings.TrimRight(strings.TrimSpace(configuration.GitLabURL), "/")
	configuration.GitLabToken = strings.TrimSpace(configuration.GitLabToken)

	p.setConfiguration(configuration)

	return nil
}
