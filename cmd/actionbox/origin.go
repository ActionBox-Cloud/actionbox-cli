package main

import (
	"os"
	"strings"
)

const (
	originRefMax   = 240
	originURLMax   = 2048
	originLabelMax = 120
)

// detectOrigin inspects well-known CI/CD environment variables and returns a
// provider-agnostic origin block for Action metadata. It returns nil when no
// recognizable environment is present so callers can fall back to flags.
func detectOrigin(environ func(string) string) map[string]any {
	env := environ
	if env == nil {
		env = os.Getenv
	}

	if github := originFromGitHub(env); github != nil {
		return github
	}
	if gitlab := originFromGitLab(env); gitlab != nil {
		return gitlab
	}
	if jenkins := originFromJenkins(env); jenkins != nil {
		return jenkins
	}
	if circle := originFromCircleCI(env); circle != nil {
		return circle
	}
	return nil
}

func originFromGitHub(env func(string) string) map[string]any {
	runID := env("GITHUB_RUN_ID")
	if runID == "" {
		return nil
	}
	repo := env("GITHUB_REPOSITORY")
	sha := env("GITHUB_SHA")
	ref := env("GITHUB_REF_NAME")
	workflow := env("GITHUB_WORKFLOW")
	attempt := env("GITHUB_RUN_ATTEMPT")
	server := env("GITHUB_SERVER_URL")
	runURL := ""
	if server != "" && repo != "" {
		runURL = server + "/" + repo + "/actions/runs/" + runID
	}
	labels := []string{}
	if repo != "" {
		labels = append(labels, repo)
	}
	if ref != "" {
		labels = append(labels, ref)
	}
	if sha != "" && len(sha) >= 7 {
		labels = append(labels, sha[:7])
	}
	parts := []string{}
	if workflow != "" {
		parts = append(parts, workflow)
	}
	parts = append(parts, "run "+runID)
	if attempt != "" {
		parts = append(parts, "attempt "+attempt)
	}
	if len(parts) == 1 {
		parts = append(parts, ref, shortSHA(sha))
	}
	return boundedOrigin("github_actions", strings.Join(parts, " · "), runURL, labels)
}

func originFromGitLab(env func(string) string) map[string]any {
	pipelineID := env("CI_PIPELINE_ID")
	if pipelineID == "" {
		return nil
	}
	project := env("CI_PROJECT_PATH")
	sha := env("CI_COMMIT_SHA")
	ref := env("CI_COMMIT_REF_NAME")
	jobName := env("CI_JOB_NAME")
	projectURL := env("CI_PROJECT_URL")
	pipelineURL := ""
	if projectURL != "" {
		pipelineURL = projectURL + "/pipelines/" + pipelineID
	}
	labels := []string{}
	if project != "" {
		labels = append(labels, project)
	}
	if ref != "" {
		labels = append(labels, ref)
	}
	if sha != "" && len(sha) >= 8 {
		labels = append(labels, sha[:8])
	}
	parts := []string{}
	if jobName != "" {
		parts = append(parts, jobName)
	}
	parts = append(parts, "pipeline "+pipelineID)
	if len(parts) == 1 {
		parts = append(parts, ref, shortSHA(sha))
	}
	return boundedOrigin("gitlab_ci", strings.Join(parts, " · "), pipelineURL, labels)
}

func originFromJenkins(env func(string) string) map[string]any {
	buildID := env("BUILD_ID")
	if buildID == "" || env("JENKINS_URL") == "" {
		return nil
	}
	jobName := env("JOB_NAME")
	buildURL := env("BUILD_URL")
	labels := []string{}
	if jobName != "" {
		labels = append(labels, jobName)
	}
	return boundedOrigin("jenkins", "build "+buildID, buildURL, labels)
}

func originFromCircleCI(env func(string) string) map[string]any {
	buildNum := env("CIRCLE_BUILD_NUM")
	if buildNum == "" {
		return nil
	}
	repo := env("CIRCLE_REPOSITORY_URL")
	ref := env("CIRCLE_BRANCH")
	sha := env("CIRCLE_SHA1")
	labels := []string{}
	if repo != "" {
		labels = append(labels, repo)
	}
	if ref != "" {
		labels = append(labels, ref)
	}
	if sha != "" && len(sha) >= 7 {
		labels = append(labels, sha[:7])
	}
	parts := []string{"build " + buildNum}
	parts = append(parts, ref, shortSHA(sha))
	return boundedOrigin("circleci", strings.Join(parts, " · "), env("CIRCLE_BUILD_URL"), labels)
}

func boundedOrigin(provider, ref, url string, labels []string) map[string]any {
	boundedLabels := make([]string, 0, len(labels))
	for _, label := range labels {
		if label = strings.TrimSpace(label); label != "" {
			boundedLabels = append(boundedLabels, truncateRunes(label, originLabelMax))
		}
	}
	return map[string]any{
		"provider": provider,
		"ref":      truncateRunes(strings.TrimSpace(ref), originRefMax),
		"url":      truncateRunes(strings.TrimSpace(url), originURLMax),
		"labels":   boundedLabels,
	}
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func shortSHA(sha string) string {
	if sha == "" {
		return ""
	}
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
