package main

import (
	"encoding/json"

	"github.com/pkg/errors"
)

const (
	channelProjectKeyPrefix = "channel_project_"
	projectChannelKeyPrefix = "project_channel_"
	summaryKeyPrefix        = "summary_"
	summaryTTLSeconds       = 300 // 5분
)

// ChannelProject represents a GitLab project linked to a Mattermost channel.
type ChannelProject struct {
	ProjectPath string `json:"project_path"`
	ProjectID   int    `json:"project_id,omitempty"`
	ProjectURL  string `json:"project_url,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
}

func (p *Plugin) storeChannelProject(channelID string, project *ChannelProject) error {
	data, err := json.Marshal(project)
	if err != nil {
		return errors.Wrap(err, "failed to marshal channel project")
	}
	if appErr := p.API.KVSet(channelProjectKeyPrefix+channelID, data); appErr != nil {
		return errors.Wrap(appErr, "failed to store channel project")
	}
	// Reverse mapping: project path → channel ID (for webhook lookup)
	if appErr := p.API.KVSet(projectChannelKeyPrefix+project.ProjectPath, []byte(channelID)); appErr != nil {
		p.API.LogWarn("Failed to store project→channel reverse mapping", "error", appErr.Error())
	}
	return nil
}

func (p *Plugin) getChannelProject(channelID string) (*ChannelProject, error) {
	data, appErr := p.API.KVGet(channelProjectKeyPrefix + channelID)
	if appErr != nil {
		return nil, errors.Wrap(appErr, "failed to get channel project")
	}
	if data == nil {
		return nil, nil
	}

	var project ChannelProject
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal channel project")
	}
	return &project, nil
}

func (p *Plugin) deleteChannelProject(channelID string) error {
	// Clean up reverse mapping before deleting forward mapping
	if project, err := p.getChannelProject(channelID); err == nil && project != nil {
		p.API.KVDelete(projectChannelKeyPrefix + project.ProjectPath)
	}
	if appErr := p.API.KVDelete(channelProjectKeyPrefix + channelID); appErr != nil {
		return errors.Wrap(appErr, "failed to delete channel project")
	}
	return nil
}

// getChannelByProject looks up the Mattermost channel ID linked to a GitLab project path.
func (p *Plugin) getChannelByProject(projectPath string) (string, error) {
	data, appErr := p.API.KVGet(projectChannelKeyPrefix + projectPath)
	if appErr != nil {
		return "", errors.Wrap(appErr, "failed to get channel by project")
	}
	return string(data), nil
}

// storeSummary saves a summary text with a 5-minute TTL.
func (p *Plugin) storeSummary(key, summary string) error {
	if appErr := p.API.KVSetWithExpiry(summaryKeyPrefix+key, []byte(summary), summaryTTLSeconds); appErr != nil {
		return errors.Wrap(appErr, "failed to store summary")
	}
	return nil
}

// getSummaryAndDelete retrieves a stored summary and immediately deletes it.
func (p *Plugin) getSummaryAndDelete(key string) (string, error) {
	data, appErr := p.API.KVGet(summaryKeyPrefix + key)
	if appErr != nil {
		return "", errors.Wrap(appErr, "failed to get summary")
	}
	if data == nil {
		return "", errors.New("요약 내용이 만료되었거나 이미 사용됐습니다 (5분 제한)")
	}
	p.API.KVDelete(summaryKeyPrefix + key)
	return string(data), nil
}
