package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/pkg/errors"
	gitlab "github.com/xanzy/go-gitlab"
)

// GitLabClient wraps the go-gitlab client with project-specific operations.
type GitLabClient struct {
	client  *gitlab.Client
	baseURL string
}

// friendlyGitLabError converts raw Go HTTP/network errors into user-readable Korean messages.
// Raw errors contain GitLab API URLs which would trigger Mattermost's OG image fetcher.
func friendlyGitLabError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no such host"),
		strings.Contains(msg, "name resolution failure"),
		strings.Contains(msg, "lookup "):
		return "GitLab 서버에 연결할 수 없습니다 (DNS 오류). 서버의 네트워크 설정을 확인하세요."
	case strings.Contains(msg, "connection refused"):
		return "GitLab 서버가 응답하지 않습니다 (connection refused)."
	case strings.Contains(msg, "i/o timeout"),
		strings.Contains(msg, "context deadline exceeded"),
		strings.Contains(msg, "TLS handshake timeout"):
		return "GitLab 서버 연결 시간이 초과되었습니다."
	case strings.Contains(msg, "certificate"),
		strings.Contains(msg, "x509"):
		return "GitLab SSL 인증서 오류. 자체 서명 인증서라면 서버 설정을 확인하세요."
	}
	// Check go-gitlab HTTP error responses
	if errResp, ok := err.(*gitlab.ErrorResponse); ok {
		switch errResp.Response.StatusCode {
		case http.StatusUnauthorized:
			return "GitLab 인증 실패 (401). 토큰을 확인하세요."
		case http.StatusForbidden:
			return "GitLab 접근 권한이 없습니다 (403)."
		case http.StatusNotFound:
			return "프로젝트를 찾을 수 없습니다 (404). 경로를 확인하세요."
		}
	}
	// Strip URLs from error message to avoid Mattermost OG fetch
	if idx := strings.Index(msg, "http"); idx != -1 {
		if colon := strings.Index(msg[idx:], ":"); colon != -1 {
			// Return the part before the URL
			prefix := strings.TrimSpace(msg[:idx])
			if prefix != "" {
				return prefix
			}
		}
	}
	return msg
}

func newGitLabClient(gitlabURL, token string) (*GitLabClient, error) {
	baseURL := strings.TrimRight(gitlabURL, "/") + "/"
	git, err := gitlab.NewClient(token, gitlab.WithBaseURL(baseURL))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create GitLab client")
	}
	return &GitLabClient{
		client:  git,
		baseURL: strings.TrimRight(gitlabURL, "/"),
	}, nil
}

func (g *GitLabClient) GetProject(projectPath string) (*gitlab.Project, error) {
	project, _, err := g.client.Projects.GetProject(projectPath, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get project")
	}
	return project, nil
}

func (g *GitLabClient) ListMergeRequests(projectPath, state string, page int) ([]*gitlab.MergeRequest, error) {
	opts := &gitlab.ListProjectMergeRequestsOptions{
		State:   gitlab.Ptr(state),
		OrderBy: gitlab.Ptr("created_at"),
		Sort:    gitlab.Ptr("desc"),
		ListOptions: gitlab.ListOptions{
			Page:    page,
			PerPage: 20,
		},
	}
	mrs, _, err := g.client.MergeRequests.ListProjectMergeRequests(projectPath, opts)
	if err != nil {
		return nil, errors.Wrap(err, "failed to list merge requests")
	}
	return mrs, nil
}

func (g *GitLabClient) GetMergeRequest(projectPath string, mrID int) (*gitlab.MergeRequest, error) {
	mr, _, err := g.client.MergeRequests.GetMergeRequest(projectPath, mrID, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get merge request")
	}
	return mr, nil
}

// MRDiff holds diff information for a single file in an MR.
type MRDiff struct {
	OldPath   string
	NewPath   string
	IsNew     bool
	IsRenamed bool
	IsDeleted bool
	Diff      string
}

func (g *GitLabClient) GetMergeRequestDiffs(projectPath string, mrID int) ([]*MRDiff, error) {
	diffs, _, err := g.client.MergeRequests.ListMergeRequestDiffs(projectPath, mrID, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get merge request diffs")
	}

	result := make([]*MRDiff, len(diffs))
	for i, d := range diffs {
		result[i] = &MRDiff{
			OldPath:   d.OldPath,
			NewPath:   d.NewPath,
			IsNew:     d.NewFile,
			IsRenamed: d.RenamedFile,
			IsDeleted: d.DeletedFile,
			Diff:      d.Diff,
		}
	}
	return result, nil
}

// formatMRList formats a list of MRs for display in Mattermost.
func formatMRList(mrs []*gitlab.MergeRequest, projectPath string) string {
	if len(mrs) == 0 {
		return "조회된 MR이 없습니다."
	}

	var sb strings.Builder
	for _, mr := range mrs {
		authorName := "unknown"
		if mr.Author != nil {
			authorName = mr.Author.Username
		}

		labels := ""
		if len(mr.Labels) > 0 {
			labels = " — " + "`" + strings.Join(mr.Labels, "` `") + "`"
		}

		stateIcon := "🟢"
		switch mr.State {
		case "merged":
			stateIcon = "✅"
		case "closed":
			stateIcon = "🔴"
		}

		sb.WriteString(fmt.Sprintf("%s **[!%d %s](%s)**%s — @%s\n",
			stateIcon, mr.IID, mr.Title, mr.WebURL, labels, authorName,
		))
	}
	return sb.String()
}

// formatMRDetails formats detailed MR information for display.
func formatMRDetails(mr *gitlab.MergeRequest) string {
	stateIcon := "🟢"
	switch mr.State {
	case "merged":
		stateIcon = "✅"
	case "closed":
		stateIcon = "🔴"
	}

	authorName := "unknown"
	if mr.Author != nil {
		authorName = "@" + mr.Author.Username
	}

	var assigneeNames []string
	for _, a := range mr.Assignees {
		assigneeNames = append(assigneeNames, "@"+a.Username)
	}

	var reviewerNames []string
	for _, r := range mr.Reviewers {
		reviewerNames = append(reviewerNames, "@"+r.Username)
	}

	desc := mr.Description
	if len(desc) > 600 {
		desc = desc[:600] + "\n\n*(설명이 너무 길어 일부만 표시됩니다)*"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s [!%d](%s)\n", stateIcon, mr.IID, mr.WebURL))
	sb.WriteString(fmt.Sprintf("### %s\n\n", mr.Title))

	if desc != "" {
		sb.WriteString(desc + "\n\n")
	}

	sb.WriteString("| 항목 | 내용 |\n|---|---|\n")
	sb.WriteString(fmt.Sprintf("| **작성자** | %s |\n", authorName))
	sb.WriteString(fmt.Sprintf("| **브랜치** | `%s` → `%s` |\n", mr.SourceBranch, mr.TargetBranch))
	sb.WriteString(fmt.Sprintf("| **상태** | %s |\n", mr.State))

	if len(assigneeNames) > 0 {
		sb.WriteString(fmt.Sprintf("| **담당자** | %s |\n", strings.Join(assigneeNames, ", ")))
	}
	if len(reviewerNames) > 0 {
		sb.WriteString(fmt.Sprintf("| **리뷰어** | %s |\n", strings.Join(reviewerNames, ", ")))
	}
	if len(mr.Labels) > 0 {
		sb.WriteString(fmt.Sprintf("| **라벨** | `%s` |\n", strings.Join(mr.Labels, "` `")))
	}
	if mr.ChangesCount != "" {
		sb.WriteString(fmt.Sprintf("| **변경 파일** | %s files |\n", mr.ChangesCount))
	}
	if mr.CreatedAt != nil {
		sb.WriteString(fmt.Sprintf("| **생성일** | %s |\n", mr.CreatedAt.Format("2006-01-02 15:04")))
	}

	return sb.String()
}

// formatDiffSummary formats a summary of changed files.
func formatDiffSummary(diffs []*MRDiff) string {
	if len(diffs) == 0 {
		return "변경된 파일이 없습니다."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("**변경된 파일 (%d개):**\n\n", len(diffs)))

	for _, d := range diffs {
		path := d.NewPath
		if d.IsDeleted {
			path = d.OldPath
		}

		status := ""
		if d.IsNew {
			status = " *(추가)*"
		} else if d.IsDeleted {
			status = " *(삭제)*"
		} else if d.IsRenamed {
			status = fmt.Sprintf(" *(이름 변경: `%s`)*", d.OldPath)
		}
		sb.WriteString(fmt.Sprintf("- `%s`%s\n", path, status))
	}

	sb.WriteString("\n특정 파일의 diff를 보려면: `/gl mr diff <id> <파일경로>`")
	return sb.String()
}

// formatFileDiff formats the diff for a specific file.
func formatFileDiff(diffs []*MRDiff, filterFile string) string {
	const maxDiffLen = 12000

	for _, d := range diffs {
		if d.NewPath == filterFile || d.OldPath == filterFile {
			var sb strings.Builder

			statusLabel := "수정됨"
			if d.IsNew {
				statusLabel = "추가됨"
			} else if d.IsDeleted {
				statusLabel = "삭제됨"
			} else if d.IsRenamed {
				statusLabel = fmt.Sprintf("이름 변경 (`%s` → `%s`)", d.OldPath, d.NewPath)
			}

			sb.WriteString(fmt.Sprintf("**`%s`** *(%s)*\n\n", d.NewPath, statusLabel))

			diffContent := d.Diff
			if diffContent == "" {
				return sb.String() + "*바이너리 파일 또는 diff 없음*"
			}

			truncated := false
			if len(diffContent) > maxDiffLen {
				diffContent = diffContent[:maxDiffLen]
				// truncate at last newline to avoid partial lines
				if idx := strings.LastIndex(diffContent, "\n"); idx > 0 {
					diffContent = diffContent[:idx]
				}
				truncated = true
			}

			sb.WriteString("```diff\n")
			sb.WriteString(diffContent)
			sb.WriteString("\n```")

			if truncated {
				sb.WriteString("\n\n*(diff가 너무 커서 일부만 표시됩니다. GitLab에서 전체를 확인하세요.)*")
			}

			return sb.String()
		}
	}

	return fmt.Sprintf("파일 `%s`를 찾을 수 없습니다.\n`/gitlab mr diff <id>`로 변경된 파일 목록을 확인하세요.", filterFile)
}
