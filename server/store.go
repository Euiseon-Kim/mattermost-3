package main

import (
	"encoding/json"

	"github.com/pkg/errors"
)

const channelProjectKeyPrefix = "channel_project_"

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
	if appErr := p.API.KVDelete(channelProjectKeyPrefix + channelID); appErr != nil {
		return errors.Wrap(appErr, "failed to delete channel project")
	}
	return nil
}
