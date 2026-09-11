package main

import (
	"strings"
	"testing"
)

func testEnv(pairs ...string) func(string) string {
	values := map[string]string{}
	for index := 0; index+1 < len(pairs); index += 2 {
		values[pairs[index]] = pairs[index+1]
	}
	return func(key string) string { return values[key] }
}

func TestDetectOriginGitHubActions(t *testing.T) {
	origin := detectOrigin(testEnv(
		"GITHUB_RUN_ID", "4821",
		"GITHUB_REPOSITORY", "acme/web",
		"GITHUB_SHA", "abcdef1234567890",
		"GITHUB_REF_NAME", "main",
		"GITHUB_WORKFLOW", "deploy",
		"GITHUB_SERVER_URL", "https://github.com",
	))
	if origin == nil {
		t.Fatal("expected origin detection")
	}
	if origin["provider"] != "github_actions" {
		t.Errorf("provider = %v, want github_actions", origin["provider"])
	}
	if origin["url"] != "https://github.com/acme/web/actions/runs/4821" {
		t.Errorf("url = %v", origin["url"])
	}
	ref, ok := origin["ref"].(string)
	if !ok || ref == "" {
		t.Errorf("ref missing: %v", origin["ref"])
	}
	labels, ok := origin["labels"].([]string)
	if !ok || len(labels) != 3 {
		t.Errorf("labels = %v, want 3 entries", origin["labels"])
	}
}

func TestDetectOriginGitLabCI(t *testing.T) {
	origin := detectOrigin(testEnv(
		"CI_PIPELINE_ID", "9001",
		"CI_PROJECT_PATH", "acme/web",
		"CI_COMMIT_SHA", "0123456789abcdef",
		"CI_COMMIT_REF_NAME", "main",
		"CI_JOB_NAME", "approve-deploy",
		"CI_PROJECT_URL", "https://gitlab.com/acme/web",
	))
	if origin == nil {
		t.Fatal("expected origin detection")
	}
	if origin["provider"] != "gitlab_ci" {
		t.Errorf("provider = %v, want gitlab_ci", origin["provider"])
	}
	if origin["url"] != "https://gitlab.com/acme/web/pipelines/9001" {
		t.Errorf("url = %v", origin["url"])
	}
}

func TestDetectOriginJenkinsAndCircleCI(t *testing.T) {
	jenkins := detectOrigin(testEnv(
		"BUILD_ID", "77",
		"JENKINS_URL", "https://ci.acme.com",
		"JOB_NAME", "deploy/web",
		"BUILD_URL", "https://ci.acme.com/job/deploy/77",
	))
	if jenkins == nil || jenkins["provider"] != "jenkins" {
		t.Fatalf("jenkins origin = %v", jenkins)
	}
	circle := detectOrigin(testEnv(
		"CIRCLE_BUILD_NUM", "55",
		"CIRCLE_BRANCH", "main",
		"CIRCLE_SHA1", "fedcba9876543210",
	))
	if circle == nil || circle["provider"] != "circleci" {
		t.Fatalf("circle origin = %v", circle)
	}
}

func TestDetectOriginNone(t *testing.T) {
	if origin := detectOrigin(testEnv("HOME", "/tmp")); origin != nil {
		t.Fatalf("expected nil origin, got %v", origin)
	}
}

func TestDetectOriginBoundsUntrustedEnvironmentValues(t *testing.T) {
	longValue := strings.Repeat("界", 3000)
	origin := detectOrigin(testEnv(
		"GITHUB_RUN_ID", longValue,
		"GITHUB_REPOSITORY", longValue,
		"GITHUB_REF_NAME", longValue,
		"GITHUB_WORKFLOW", longValue,
		"GITHUB_SERVER_URL", "https://github.example/"+longValue,
	))
	if origin == nil {
		t.Fatal("expected origin detection")
	}
	if got := len([]rune(origin["ref"].(string))); got > originRefMax {
		t.Fatalf("ref length = %d, want <= %d", got, originRefMax)
	}
	if got := len([]rune(origin["url"].(string))); got > originURLMax {
		t.Fatalf("url length = %d, want <= %d", got, originURLMax)
	}
	for _, label := range origin["labels"].([]string) {
		if got := len([]rune(label)); got > originLabelMax {
			t.Fatalf("label length = %d, want <= %d", got, originLabelMax)
		}
	}
}
