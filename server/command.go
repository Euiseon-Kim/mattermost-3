package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

const commandTrigger = "gl"

const helpText = `**GitLab Code Review 플러그인 명령어**

**MR 조회**
* ` + "`/gl mr list [state]`" + ` — MR 목록 조회 (state: open, merged, closed, all | 기본값: open)
* ` + "`/gl mr <id>`" + ` — MR 상세 정보 조회
* ` + "`/gl mr diff <id>`" + ` — MR 변경 파일 목록 조회
* ` + "`/gl mr diff <id> <파일경로>`" + ` — 특정 파일의 diff 조회
* ` + "`/gl mr summarize <id>`" + ` — 채널 대화를 AI로 요약 후 MR 코멘트 등록

**프로젝트 연결**
* ` + "`/gl project link <project-path>`" + ` — 현재 채널을 GitLab 프로젝트에 연결 (예: ` + "`group/myproject`" + `)
* ` + "`/gl project unlink`" + ` — 채널의 GitLab 프로젝트 연결 해제
* ` + "`/gl project info`" + ` — 연결된 프로젝트 정보 조회

**채널 생성**
* ` + "`/gl channel create <project-path>`" + ` — GitLab 프로젝트용 채널 자동 생성 및 연결

* ` + "`/gl help`" + ` — 이 도움말 표시`

func (p *Plugin) registerCommands() error {
	return p.API.RegisterCommand(&model.Command{
		Trigger:          commandTrigger,
		AutoComplete:     true,
		AutoCompleteDesc: "GitLab 코드리뷰 통합 명령어",
		AutoCompleteHint: "[mr|project|channel|help]",
		DisplayName:      "GitLab Code Review",
		AutocompleteData: buildAutocompleteData(),
	})
}

func buildAutocompleteData() *model.AutocompleteData {
	root := model.NewAutocompleteData(commandTrigger, "[명령어]", "GitLab Code Review 명령어")

	mr := model.NewAutocompleteData("mr", "[list|<id>|diff]", "Merge Request 조회")
	mrList := model.NewAutocompleteData("list", "[open|merged|closed|all]", "MR 목록 조회")
	mrList.AddStaticListArgument("state", false, []model.AutocompleteListItem{
		{Item: "open", HelpText: "열린 MR"},
		{Item: "merged", HelpText: "머지된 MR"},
		{Item: "closed", HelpText: "닫힌 MR"},
		{Item: "all", HelpText: "전체 MR"},
	})
	mr.AddCommand(mrList)
	mr.AddCommand(model.NewAutocompleteData("diff", "<id> [파일경로]", "MR diff 조회"))
	mr.AddCommand(model.NewAutocompleteData("summarize", "<id>", "채널 대화 AI 요약 후 MR 코멘트 등록"))
	root.AddCommand(mr)

	project := model.NewAutocompleteData("project", "[link|unlink|info]", "프로젝트 연결 관리")
	project.AddCommand(model.NewAutocompleteData("link", "<project-path>", "채널을 GitLab 프로젝트에 연결"))
	project.AddCommand(model.NewAutocompleteData("unlink", "", "채널의 프로젝트 연결 해제"))
	project.AddCommand(model.NewAutocompleteData("info", "", "연결된 프로젝트 정보 조회"))
	root.AddCommand(project)

	channel := model.NewAutocompleteData("channel", "create <project-path>", "GitLab 프로젝트용 채널 생성")
	channel.AddCommand(model.NewAutocompleteData("create", "<project-path>", "프로젝트용 채널 생성 및 연결"))
	root.AddCommand(channel)

	root.AddCommand(model.NewAutocompleteData("help", "", "도움말 표시"))

	return root
}

func (p *Plugin) ExecuteCommand(c *plugin.Context, args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	fields := strings.Fields(args.Command)
	if len(fields) == 0 {
		return p.ephemeral(helpText), nil
	}

	subcommand := ""
	if len(fields) >= 2 {
		subcommand = fields[1]
	}

	switch subcommand {
	case "mr":
		return p.handleMRCommand(args, fields[2:])
	case "project":
		return p.handleProjectCommand(args, fields[2:])
	case "channel":
		return p.handleChannelCommand(args, fields[2:])
	case "help":
		return p.ephemeral(helpText), nil
	default:
		return p.ephemeral(helpText), nil
	}
}

// --- MR commands ---

func (p *Plugin) handleMRCommand(args *model.CommandArgs, params []string) (*model.CommandResponse, *model.AppError) {
	if len(params) == 0 {
		return p.ephemeral("사용법: `/gl mr list|<id>|diff`"), nil
	}

	config := p.getConfiguration()
	if err := config.IsValid(); err != nil {
		return p.ephemeral("플러그인이 설정되지 않았습니다. 시스템 관리자에게 문의하세요."), nil
	}

	project, err := p.getChannelProject(args.ChannelId)
	if err != nil {
		return p.ephemeral("프로젝트 정보 조회 실패: " + err.Error()), nil
	}
	if project == nil {
		return p.ephemeral("이 채널은 GitLab 프로젝트에 연결되지 않았습니다.\n`/gl project link <project-path>` 명령어로 연결하거나, `/gl channel create <project-path>`로 새 채널을 만드세요."), nil
	}

	glClient, err := newGitLabClient(config.APIBaseURL(), config.GitLabToken)
	if err != nil {
		return p.ephemeral("GitLab 연결 실패: " + friendlyGitLabError(err)), nil
	}

	switch params[0] {
	case "list":
		return p.handleMRList(project, glClient, params[1:])
	case "diff":
		if len(params) < 2 {
			return p.ephemeral("사용법: `/gl mr diff <id> [파일경로]`"), nil
		}
		mrID, err := strconv.Atoi(params[1])
		if err != nil {
			return p.ephemeral("유효하지 않은 MR ID: " + params[1]), nil
		}
		filterFile := strings.Join(params[2:], " ")
		return p.handleMRDiff(project, glClient, mrID, filterFile)
	case "summarize":
		if len(params) < 2 {
			return p.ephemeral("사용법: `/gl mr summarize <id>`"), nil
		}
		mrID, err := strconv.Atoi(params[1])
		if err != nil {
			return p.ephemeral("유효하지 않은 MR ID: " + params[1]), nil
		}
		return p.handleMRSummarize(args, project, glClient, mrID)
	default:
		mrID, err := strconv.Atoi(params[0])
		if err != nil {
			return p.ephemeral(fmt.Sprintf("알 수 없는 명령어: `%s`\n`/gl help`로 도움말을 확인하세요.", params[0])), nil
		}
		return p.handleMRDetails(project, glClient, mrID)
	}
}

func (p *Plugin) handleMRList(project *ChannelProject, glClient *GitLabClient, params []string) (*model.CommandResponse, *model.AppError) {
	state := "opened"
	if len(params) >= 1 {
		switch params[0] {
		case "open", "opened":
			state = "opened"
		case "merged":
			state = "merged"
		case "closed":
			state = "closed"
		case "all":
			state = "all"
		default:
			return p.ephemeral(fmt.Sprintf("알 수 없는 상태: `%s`. 사용 가능: open, merged, closed, all", params[0])), nil
		}
	}

	mrs, err := glClient.ListMergeRequests(project.ProjectPath, state, 1)
	if err != nil {
		return p.ephemeral("MR 목록 조회 실패: " + friendlyGitLabError(err)), nil
	}

	stateLabel := state
	if state == "opened" {
		stateLabel = "open"
	}

	header := fmt.Sprintf("### Merge Requests — [%s](%s) (%s)\n\n",
		project.ProjectName, project.ProjectURL, stateLabel)
	body := formatMRList(mrs, project.ProjectPath)

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeInChannel,
		Text:         header + body,
	}, nil
}

func (p *Plugin) handleMRDetails(project *ChannelProject, glClient *GitLabClient, mrID int) (*model.CommandResponse, *model.AppError) {
	mr, err := glClient.GetMergeRequest(project.ProjectPath, mrID)
	if err != nil {
		return p.ephemeral(fmt.Sprintf("MR !%d 조회 실패: %s", mrID, friendlyGitLabError(err))), nil
	}

	siteURL := ""
	if cfg := p.API.GetConfig(); cfg != nil && cfg.ServiceSettings.SiteURL != nil {
		siteURL = *cfg.ServiceSettings.SiteURL
	}

	var attachments []*model.SlackAttachment
	diffs, err := glClient.GetMergeRequestDiffs(project.ProjectPath, mrID)
	if err != nil {
		p.API.LogWarn("MR diff 조회 실패", "mr_id", mrID, "error", err.Error())
	} else {
		attachments = buildFileButtons(diffs, project.ProjectPath, mr.IID, siteURL)
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeInChannel,
		Text:         formatMRDetails(mr),
		Attachments:  attachments,
	}, nil
}

func (p *Plugin) handleMRDiff(project *ChannelProject, glClient *GitLabClient, mrID int, filterFile string) (*model.CommandResponse, *model.AppError) {
	diffs, err := glClient.GetMergeRequestDiffs(project.ProjectPath, mrID)
	if err != nil {
		return p.ephemeral(fmt.Sprintf("MR !%d diff 조회 실패: %s", mrID, friendlyGitLabError(err))), nil
	}

	mr, err := glClient.GetMergeRequest(project.ProjectPath, mrID)
	if err != nil {
		return p.ephemeral(fmt.Sprintf("MR !%d 조회 실패: %s", mrID, friendlyGitLabError(err))), nil
	}

	header := fmt.Sprintf("### Diff — [!%d %s](%s)\n\n", mr.IID, mr.Title, mr.WebURL)

	var body string
	if filterFile == "" {
		body = formatDiffSummary(diffs)
	} else {
		body = formatFileDiff(diffs, filterFile)
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeInChannel,
		Text:         header + body,
	}, nil
}

// --- Project commands ---

func (p *Plugin) handleProjectCommand(args *model.CommandArgs, params []string) (*model.CommandResponse, *model.AppError) {
	if len(params) == 0 {
		return p.ephemeral("사용법: `/gl project link|unlink|info`"), nil
	}

	switch params[0] {
	case "link":
		if len(params) < 2 {
			return p.ephemeral("사용법: `/gl project link <project-path>`\n예시: `/gl project link group/myproject`"), nil
		}
		return p.handleProjectLink(args, params[1])
	case "unlink":
		return p.handleProjectUnlink(args)
	case "info":
		return p.handleProjectInfo(args)
	default:
		return p.ephemeral(fmt.Sprintf("알 수 없는 project 명령어: `%s`. 사용 가능: link, unlink, info", params[0])), nil
	}
}

func (p *Plugin) handleProjectLink(args *model.CommandArgs, projectPath string) (*model.CommandResponse, *model.AppError) {
	config := p.getConfiguration()
	if err := config.IsValid(); err != nil {
		return p.ephemeral("플러그인이 설정되지 않았습니다. 시스템 관리자에게 문의하세요."), nil
	}

	glClient, err := newGitLabClient(config.APIBaseURL(), config.GitLabToken)
	if err != nil {
		return p.ephemeral("GitLab 연결 실패: " + friendlyGitLabError(err)), nil
	}

	gitProject, err := glClient.GetProject(projectPath)
	if err != nil {
		return p.ephemeral(fmt.Sprintf("프로젝트 `%s`` 조회 실패: %s", projectPath, friendlyGitLabError(err))), nil
	}

	project := &ChannelProject{
		ProjectPath: projectPath,
		ProjectID:   gitProject.ID,
		ProjectURL:  gitProject.WebURL,
		ProjectName: gitProject.NameWithNamespace,
	}

	if err := p.storeChannelProject(args.ChannelId, project); err != nil {
		return p.ephemeral("프로젝트 연결 저장 실패: " + err.Error()), nil
	}

	if ch, appErr := p.API.GetChannel(args.ChannelId); appErr == nil {
		ch.Header = fmt.Sprintf("GitLab: [%s](%s)", gitProject.NameWithNamespace, gitProject.WebURL)
		p.API.UpdateChannel(ch)
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeInChannel,
		Text: fmt.Sprintf("✅ 이 채널이 **[%s](%s)** 에 연결되었습니다.\n\n`/gl mr list`로 MR 목록을 조회할 수 있습니다.",
			gitProject.NameWithNamespace, gitProject.WebURL),
	}, nil
}

func (p *Plugin) handleProjectUnlink(args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	project, err := p.getChannelProject(args.ChannelId)
	if err != nil {
		return p.ephemeral("프로젝트 정보 조회 실패: " + err.Error()), nil
	}
	if project == nil {
		return p.ephemeral("이 채널은 GitLab 프로젝트에 연결되지 않았습니다."), nil
	}

	projectName := project.ProjectName
	if projectName == "" {
		projectName = project.ProjectPath
	}

	if err := p.deleteChannelProject(args.ChannelId); err != nil {
		return p.ephemeral("프로젝트 연결 해제 실패: " + err.Error()), nil
	}

	if ch, appErr := p.API.GetChannel(args.ChannelId); appErr == nil {
		ch.Header = ""
		p.API.UpdateChannel(ch)
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeInChannel,
		Text:         fmt.Sprintf("🔌 **%s** 프로젝트 연결이 해제되었습니다.", projectName),
	}, nil
}

func (p *Plugin) handleProjectInfo(args *model.CommandArgs) (*model.CommandResponse, *model.AppError) {
	project, err := p.getChannelProject(args.ChannelId)
	if err != nil {
		return p.ephemeral("프로젝트 정보 조회 실패: " + err.Error()), nil
	}
	if project == nil {
		return p.ephemeral("이 채널은 GitLab 프로젝트에 연결되지 않았습니다.\n`/gl project link <project-path>` 명령어로 연결하세요."), nil
	}

	text := fmt.Sprintf("**연결된 GitLab 프로젝트:** [%s](%s)\n**경로:** `%s`",
		project.ProjectName, project.ProjectURL, project.ProjectPath)

	return p.ephemeral(text), nil
}

// --- Channel commands ---

func (p *Plugin) handleChannelCommand(args *model.CommandArgs, params []string) (*model.CommandResponse, *model.AppError) {
	if len(params) == 0 || params[0] != "create" {
		return p.ephemeral("사용법: `/gl channel create <project-path>`"), nil
	}
	if len(params) < 2 {
		return p.ephemeral("사용법: `/gl channel create <project-path>`\n예시: `/gl channel create group/myproject`"), nil
	}
	return p.handleChannelCreate(args, params[1])
}

func (p *Plugin) handleChannelCreate(args *model.CommandArgs, projectPath string) (*model.CommandResponse, *model.AppError) {
	config := p.getConfiguration()
	if err := config.IsValid(); err != nil {
		return p.ephemeral("플러그인이 설정되지 않았습니다. 시스템 관리자에게 문의하세요."), nil
	}

	glClient, err := newGitLabClient(config.APIBaseURL(), config.GitLabToken)
	if err != nil {
		return p.ephemeral("GitLab 연결 실패: " + friendlyGitLabError(err)), nil
	}

	gitProject, err := glClient.GetProject(projectPath)
	if err != nil {
		return p.ephemeral(fmt.Sprintf("프로젝트 `%s`` 조회 실패: %s", projectPath, friendlyGitLabError(err))), nil
	}

	channelName := sanitizeChannelName(gitProject.Path)
	channelDisplayName := gitProject.Name + " (GitLab)"

	// 채널이 이미 존재하면 연결만 수행
	existingCh, appErr := p.API.GetChannelByName(args.TeamId, channelName, false)
	if appErr == nil && existingCh != nil {
		project := &ChannelProject{
			ProjectPath: projectPath,
			ProjectID:   gitProject.ID,
			ProjectURL:  gitProject.WebURL,
			ProjectName: gitProject.NameWithNamespace,
		}
		if err := p.storeChannelProject(existingCh.Id, project); err != nil {
			return p.ephemeral("프로젝트 연결 저장 실패: " + err.Error()), nil
		}
		return p.ephemeral(fmt.Sprintf("채널 **%s**이(가) 이미 존재합니다. **[%s](%s)** 프로젝트에 연결했습니다.",
			channelDisplayName, gitProject.NameWithNamespace, gitProject.WebURL)), nil
	}

	header := fmt.Sprintf("GitLab: [%s](%s)", gitProject.NameWithNamespace, gitProject.WebURL)
	purpose := fmt.Sprintf("GitLab 프로젝트 코드리뷰 채널: %s", gitProject.NameWithNamespace)

	newChannel := &model.Channel{
		TeamId:      args.TeamId,
		Type:        model.ChannelTypeOpen,
		DisplayName: channelDisplayName,
		Name:        channelName,
		Header:      header,
		Purpose:     purpose,
	}

	createdChannel, appErr := p.API.CreateChannel(newChannel)
	if appErr != nil {
		return p.ephemeral("채널 생성 실패: " + appErr.Message), nil
	}

	p.API.AddChannelMember(createdChannel.Id, args.UserId)

	project := &ChannelProject{
		ProjectPath: projectPath,
		ProjectID:   gitProject.ID,
		ProjectURL:  gitProject.WebURL,
		ProjectName: gitProject.NameWithNamespace,
	}
	if err := p.storeChannelProject(createdChannel.Id, project); err != nil {
		return p.ephemeral("채널은 생성됐지만 프로젝트 연결 저장 실패: " + err.Error()), nil
	}

	return p.ephemeral(fmt.Sprintf("✅ **%s** 채널이 생성되고 **[%s](%s)** 프로젝트에 연결되었습니다.\n해당 채널에서 `/gl mr list`로 MR을 조회할 수 있습니다.",
		channelDisplayName, gitProject.NameWithNamespace, gitProject.WebURL)), nil
}

// --- helpers ---

func sanitizeChannelName(name string) string {
	name = strings.ToLower(name)
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}

	result := sb.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if result == "" {
		result = "gitlab-project"
	}
	result = "gl-" + result
	if len(result) > 64 {
		result = result[:64]
	}
	return result
}

func (p *Plugin) ephemeral(message string) *model.CommandResponse {
	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Text:         message,
	}
}

// handleMRSummarize summarizes recent channel messages via Fabrix API,
// then shows a preview with a confirm button to post it as a GitLab MR comment.
func (p *Plugin) handleMRSummarize(args *model.CommandArgs, project *ChannelProject, glClient *GitLabClient, mrID int) (*model.CommandResponse, *model.AppError) {
	config := p.getConfiguration()
	if config.FabrixAPIURL == "" {
		return p.ephemeral("Fabrix API URL이 설정되지 않았습니다. 시스템 관리자에게 문의하세요."), nil
	}

	// Verify MR exists
	mr, err := glClient.GetMergeRequest(project.ProjectPath, mrID)
	if err != nil {
		return p.ephemeral(fmt.Sprintf("MR !%d 조회 실패: %s", mrID, friendlyGitLabError(err))), nil
	}

	// Fetch last 50 posts from channel
	postList, appErr := p.API.GetPostsForChannel(args.ChannelId, 0, 50)
	if appErr != nil {
		return p.ephemeral("채널 메시지 조회 실패: " + appErr.Error()), nil
	}

	// Format posts as conversation text (oldest first), excluding bot and system posts
	var lines []string
	for i := len(postList.Order) - 1; i >= 0; i-- {
		post := postList.Posts[postList.Order[i]]
		if post.Message == "" || post.Type != "" {
			continue
		}
		// Skip bot posts (MR notifications) and slash command responses
		if post.UserId == p.botUserID || post.Props["from_webhook"] == "true" {
			continue
		}
		user, uErr := p.API.GetUser(post.UserId)
		username := "unknown"
		if uErr == nil {
			username = user.Username
		}
		lines = append(lines, username+": "+post.Message)
	}

	if len(lines) == 0 {
		return p.ephemeral("요약할 사람의 대화 메시지가 없습니다. 채널에서 MR에 대한 논의가 있어야 요약이 가능합니다."), nil
	}

	conversationText := strings.Join(lines, "\n")
	prompt := fmt.Sprintf("User: 아래는 Mattermost 채널에서 나눈 실제 대화 내용이야. 이 대화만을 바탕으로 MR !%d '%s'에 대한 주요 논의, 결정 사항, 피드백을 한국어로 간결하게 요약해줘. 아래 대화 외의 정보는 사용하지 마:\n\n%s",
		mr.IID, mr.Title, conversationText)

	// Call Fabrix API
	summary, err := callFabrix(config.FabrixAPIURL, config.FabrixAPIKey, args.ChannelId, prompt)
	if err != nil {
		return p.ephemeral("AI 요약 실패: " + err.Error()), nil
	}

	// Store summary temporarily (5-minute TTL)
	summaryKey := uuid.New().String()
	if err := p.storeSummary(summaryKey, summary); err != nil {
		return p.ephemeral("요약 저장 실패: " + err.Error()), nil
	}

	siteURL := ""
	if cfg := p.API.GetConfig(); cfg != nil && cfg.ServiceSettings.SiteURL != nil {
		siteURL = *cfg.ServiceSettings.SiteURL
	}
	actionURL := siteURL + "/plugins/" + pluginID + "/action/post-comment"

	attachment := &model.SlackAttachment{
		Title: fmt.Sprintf("AI 요약 미리보기 — MR !%d: %s", mr.IID, mr.Title),
		Text:  summary,
		Color: "#1aaa55",
		Actions: []*model.PostAction{
			{
				Name: fmt.Sprintf("MR !%d에 코멘트 달기", mr.IID),
				Type: model.PostActionTypeButton,
				Style: "primary",
				Integration: &model.PostActionIntegration{
					URL: actionURL,
					Context: map[string]interface{}{
						"summary_key":  summaryKey,
						"project_path": project.ProjectPath,
						"mr_id":        mr.IID,
					},
				},
			},
			{
				Name:  "취소",
				Type:  model.PostActionTypeButton,
				Style: "danger",
				Integration: &model.PostActionIntegration{
					URL:     actionURL,
					Context: map[string]interface{}{"cancel": true, "summary_key": summaryKey},
				},
			},
		},
	}

	return &model.CommandResponse{
		ResponseType: model.CommandResponseTypeEphemeral,
		Attachments:  []*model.SlackAttachment{attachment},
	}, nil
}
