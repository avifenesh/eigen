package llm

import "testing"

func TestNewImageGeneratorDefaults(t *testing.T) {
	t.Setenv("EIGEN_IMAGE_MODEL", "")
	g, ok := NewImageGenerator()
	if !ok {
		t.Fatal("image generator should construct")
	}
	if g.ModelID() != "stability.stable-image-core-v1:1" {
		t.Fatalf("default model = %q", g.ModelID())
	}
}

func TestAspectRatio(t *testing.T) {
	cases := map[[2]int]string{
		{1024, 1024}: "1:1",
		{1920, 1080}: "16:9",
		{1080, 1920}: "9:16",
		{0, 0}:       "1:1",
	}
	for wh, want := range cases {
		if got := aspectRatio(wh[0], wh[1]); got != want {
			t.Errorf("aspectRatio(%d,%d) = %q, want %q", wh[0], wh[1], got, want)
		}
	}
}
