package main

import (
	"strings"

	"github.com/pkg/errors"
)

// Configuration captures the plugin's external configuration as exposed in the Mattermost server
// configuration, as well as values computed from the configuration.
type Configuration struct {
	GitLabURL            string `json:"GitLabURL"`
	GitLabInternalURL    string `json:"GitLabInternalURL"`
	GitLabToken          string `json:"GitLabToken"`
	WebhookSecret        string `json:"WebhookSecret"`
	FabrixAPIURL         string `json:"FabrixAPIURL"`
	FabrixAPIKey         string `json:"FabrixAPIKey"`
	TeamProjectMappings  string `json:"TeamProjectMappings"`
}

// APIBaseURL returns the URL to use for GitLab API calls.
// Falls back to GitLabURL when GitLabInternalURL is not set.
func (c *Configuration) APIBaseURL() string {
	if c.GitLabInternalURL != "" {
		return c.GitLabInternalURL
	}
	return c.GitLabURL
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

// ParseTeamProjectMappings parses the TeamProjectMappings text into a slice of TeamProjectMapping.
// Format: one "teamname:group/project" per line. Lines starting with '#' or blank are skipped.
func (c *Configuration) ParseTeamProjectMappings() []TeamProjectMapping {
	var result []TeamProjectMapping
	for _, line := range strings.Split(c.TeamProjectMappings, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx < 1 || idx == len(line)-1 {
			continue
		}
		teamName := strings.TrimSpace(line[:idx])
		projectPath := strings.TrimSpace(line[idx+1:])
		if teamName != "" && projectPath != "" {
			result = append(result, TeamProjectMapping{TeamName: teamName, ProjectPath: projectPath})
		}
	}
	return result
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
	configuration.GitLabInternalURL = strings.TrimRight(strings.TrimSpace(configuration.GitLabInternalURL), "/")
	configuration.GitLabToken = strings.TrimSpace(configuration.GitLabToken)
	configuration.TeamProjectMappings = strings.TrimSpace(configuration.TeamProjectMappings)

	p.setConfiguration(configuration)

	go p.provisionTeamChannels(configuration)

	return nil
}
