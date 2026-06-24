package main

import (
	"fmt"

	"github.com/mattermost/mattermost/server/public/model"
)

// TeamProjectMapping represents one line from the TeamProjectMappings config field.
type TeamProjectMapping struct {
	TeamName    string
	ProjectPath string
}

// provisionTeamChannels is called asynchronously from OnConfigurationChange.
// It iterates every configured mapping and ensures the corresponding channel exists and is linked.
func (p *Plugin) provisionTeamChannels(config *Configuration) {
	if err := config.IsValid(); err != nil {
		p.API.LogDebug("provisionTeamChannels: config not valid, skipping", "reason", err.Error())
		return
	}

	mappings := config.ParseTeamProjectMappings()
	if len(mappings) == 0 {
		return
	}

	glClient, err := newGitLabClient(config.APIBaseURL(), config.GitLabToken)
	if err != nil {
		p.API.LogError("provisionTeamChannels: GitLab 클라이언트 생성 실패", "error", err.Error())
		return
	}

	for _, m := range mappings {
		if err := p.provisionChannelForTeam(m.TeamName, m.ProjectPath, glClient); err != nil {
			p.API.LogWarn("provisionTeamChannels: 채널 프로비저닝 실패",
				"team", m.TeamName, "project", m.ProjectPath, "error", err.Error())
		}
	}
}

// provisionChannelForTeam ensures a channel linked to the given GitLab project exists in the
// given Mattermost team. Safe to call multiple times with the same arguments (idempotent).
func (p *Plugin) provisionChannelForTeam(teamName, projectPath string, glClient *GitLabClient) error {
	// 1. Mattermost 팀 조회
	team, appErr := p.API.GetTeamByName(teamName)
	if appErr != nil {
		return fmt.Errorf("팀 %q 조회 실패: %s", teamName, appErr.Error())
	}

	// 2. GitLab 프로젝트 조회
	gitProject, err := glClient.GetProject(projectPath)
	if err != nil {
		return fmt.Errorf("GitLab 프로젝트 %q 조회 실패: %s", projectPath, friendlyGitLabError(err))
	}

	channelName := sanitizeChannelName(gitProject.Path)

	channelProject := &ChannelProject{
		ProjectPath: projectPath,
		ProjectID:   gitProject.ID,
		ProjectURL:  gitProject.WebURL,
		ProjectName: gitProject.NameWithNamespace,
	}

	// 3. 채널이 이미 존재하는지 확인
	existingCh, appErr := p.API.GetChannelByName(team.Id, channelName, false)
	if appErr == nil && existingCh != nil {
		existing, _ := p.getChannelProject(existingCh.Id)
		if existing != nil && existing.ProjectPath == projectPath {
			p.API.LogDebug("provisionChannelForTeam: 이미 연결됨, 건너뜀",
				"channel", channelName, "project", projectPath)
			return nil
		}
		// 채널은 있지만 연결이 없거나 다른 프로젝트에 연결됨 — 재연결
		return p.storeChannelProject(existingCh.Id, channelProject)
	}

	// 4. 채널 생성
	newChannel := &model.Channel{
		TeamId:      team.Id,
		Type:        model.ChannelTypeOpen,
		DisplayName: gitProject.Name + " (GitLab)",
		Name:        channelName,
		Header:      fmt.Sprintf("GitLab: [%s](%s)", gitProject.NameWithNamespace, gitProject.WebURL),
		Purpose:     fmt.Sprintf("GitLab 프로젝트 코드리뷰 채널: %s", gitProject.NameWithNamespace),
	}
	createdChannel, appErr := p.API.CreateChannel(newChannel)
	if appErr != nil {
		return fmt.Errorf("채널 생성 실패: %s", appErr.Error())
	}

	// 5. 봇을 채널에 추가 (웹훅 알림 전송용)
	if p.botUserID != "" {
		if _, addErr := p.API.AddChannelMember(createdChannel.Id, p.botUserID); addErr != nil {
			p.API.LogWarn("provisionChannelForTeam: 봇 채널 추가 실패",
				"channel", channelName, "error", addErr.Error())
		}
	}

	// 6. KV 스토어에 채널-프로젝트 링크 저장
	if err := p.storeChannelProject(createdChannel.Id, channelProject); err != nil {
		return fmt.Errorf("프로젝트 연결 저장 실패: %w", err)
	}

	p.API.LogInfo("provisionChannelForTeam: 채널 생성 및 연결 완료",
		"channel", channelName, "team", teamName, "project", projectPath)
	return nil
}
