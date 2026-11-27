package utiles

import (
	"testing"

	dockerImage "github.com/docker/docker/api/types/image"
	MyType "github.com/onlyLTY/dockerCopilot/internal/types"
)

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.1.3", "v2.1.2", 1},
		{"v2.1.2", "v2.1.3", -1},
		{"v2.1.3", "v2.1.3", 0},
		{"2.1.3", "v2.1.3", 0},
		// 字符串比较会误判成"有更新"，数值比较必须得出更旧。
		{"v1.9.0", "v2.0.0", -1},
		{"v2.10.0", "v2.9.0", 1},
		{"v2.1.3-UGREEN", "v2.1.3", 1},
	}
	for _, c := range cases {
		if got := CompareVersion(c.a, c.b); got != c.want {
			t.Errorf("CompareVersion(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSplitImageNameAndTag(t *testing.T) {
	cases := []struct {
		ref      string
		wantName string
		wantTag  string
	}{
		{"nginx:latest", "nginx", "latest"},
		{"ghcr.io/foo/bar:v1", "ghcr.io/foo/bar", "v1"},
		// 带端口的私有仓库：旧实现会拆成 reg / 5000/foo:tag。
		{"reg.example.com:5000/foo:tag", "reg.example.com:5000/foo", "tag"},
		{"reg.example.com:5000/foo", "reg.example.com:5000/foo", "None"},
		{"nginx", "nginx", "None"},
	}
	for _, c := range cases {
		list := []MyType.Image{{Summary: dockerImage.Summary{RepoTags: []string{c.ref}}}}
		got := splitImageNameAndTag(list)[0]
		if got.ImageName != c.wantName || got.ImageTag != c.wantTag {
			t.Errorf("splitImageNameAndTag(%q) = (%q, %q), want (%q, %q)",
				c.ref, got.ImageName, got.ImageTag, c.wantName, c.wantTag)
		}
	}
}

func TestSplitImageNameAndTagFallsBackToDigest(t *testing.T) {
	list := []MyType.Image{{Summary: dockerImage.Summary{
		RepoDigests: []string{"nginx@sha256:abc"},
	}}}
	got := splitImageNameAndTag(list)[0]
	if got.ImageName != "nginx" || got.ImageTag != "None" {
		t.Fatalf("got (%q, %q), want (nginx, None)", got.ImageName, got.ImageTag)
	}
}
