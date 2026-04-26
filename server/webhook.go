package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/mattermost/mattermost/server/public/model"
)

// GitLab webhook event structs

type gitlabUser struct {
	Name     string `json:"name"`
	Username string `json:"username"`
}

type gitlabProject struct {
	Name              string `json:"name"`
	WebURL            string `json:"web_url"`
	PathWithNamespace string `json:"path_with_namespace"`
}

type gitlabMRAttributes struct {
	IID          int    `json:"iid"`
	Title        string `json:"title"`
	State        string `json:"state"`
	Action       string `json:"action"`
	URL          string `json:"url"`
	SourceBranch string `json:"source_branch"`
	TargetBranch string `json:"target_branch"`
}

type gitlabMREvent struct {
	User             gitlabUser         `json:"user"`
	Project          gitlabProject      `json:"project"`
	ObjectAttributes gitlabMRAttributes `json:"object_attributes"`
	Assignees        []gitlabUser       `json:"assignees"`
	Reviewers        []gitlabUser       `json:"reviewers"`
}

func (p *Plugin) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Validate webhook secret
	config := p.getConfiguration()
	if config.WebhookSecret != "" {
		if r.Header.Get("X-Gitlab-Token") != config.WebhookSecret {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	} else {
		p.API.LogWarn("Webhook secret is not configured — all webhook requests are accepted")
	}

	switch r.Header.Get("X-Gitlab-Event") {
	case "Merge Request Hook":
		p.handleMRWebhook(w, r)
	default:
		w.WriteHeader(http.StatusOK)
	}
}

func (p *Plugin) handleMRWebhook(w http.ResponseWriter, r *http.Request) {
	var event gitlabMREvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	channelID, err := p.getChannelByProject(event.Project.PathWithNamespace)
	if err != nil || channelID == "" {
		// 연결된 채널 없음 — 무시
		w.WriteHeader(http.StatusOK)
		return
	}

	msg := formatMRNotification(&event)
	if msg == "" {
		w.WriteHeader(http.StatusOK)
		return
	}

	if p.botUserID == "" {
		p.API.LogWarn("Bot user not initialized — webhook notification skipped. Check that bot account creation is enabled in System Console.")
		w.WriteHeader(http.StatusOK)
		return
	}

	post := &model.Post{
		ChannelId: channelID,
		UserId:    p.botUserID,
		Message:   msg,
	}
	if _, appErr := p.API.CreatePost(post); appErr != nil {
		p.API.LogError("Failed to post MR notification", "error", appErr.Error())
	}

	w.WriteHeader(http.StatusOK)
}

func formatMRNotification(event *gitlabMREvent) string {
	attr := event.ObjectAttributes

	var icon, label string
	switch attr.Action {
	case "open":
		icon, label = "🟢", "MR 오픈"
	case "merge":
		icon, label = "✅", "MR 머지됨"
	case "close":
		icon, label = "🔴", "MR 닫힘"
	case "reopen":
		icon, label = "🔄", "MR 다시 오픈"
	case "approved":
		icon, label = "👍", "MR 승인됨"
	case "unapproved":
		icon, label = "👎", "MR 승인 취소"
	case "update":
		// 업데이트는 너무 빈번하므로 알림 생략
		return ""
	default:
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("#### %s %s: [!%d %s](%s)\n",
		icon, label, attr.IID, attr.Title, attr.URL))
	sb.WriteString(fmt.Sprintf("**프로젝트:** [%s](%s) | **작성자:** @%s | **브랜치:** `%s` → `%s`",
		event.Project.PathWithNamespace, event.Project.WebURL,
		event.User.Username, attr.SourceBranch, attr.TargetBranch))

	if len(event.Assignees) > 0 {
		names := make([]string, len(event.Assignees))
		for i, a := range event.Assignees {
			names[i] = "@" + a.Username
		}
		sb.WriteString(fmt.Sprintf(" | **담당자:** %s", strings.Join(names, ", ")))
	}
	if len(event.Reviewers) > 0 {
		names := make([]string, len(event.Reviewers))
		for i, rv := range event.Reviewers {
			names[i] = "@" + rv.Username
		}
		sb.WriteString(fmt.Sprintf(" | **리뷰어:** %s", strings.Join(names, ", ")))
	}

	return sb.String()
}
