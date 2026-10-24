package assistant

import "testing"

func TestParseOpenUIMore(t *testing.T) {
	f := ParseOpenUI(`root = Files([FileCard("/d/a.pdf", "季度", "2 MB", "昨天"), "/d/b.png", {path: "/d/c", name: "C", mtime: "今天"}, FileCard("")], "大文件")`)
	if f == nil || f.Kind != "files" || f.Text != "大文件" || len(f.Items) != 3 ||
		f.Items[0].Sub != "季度" || f.Items[0].Label != "2 MB" || f.Items[1].Path != "/d/b.png" || f.Items[2].Placeholder != "今天" {
		t.Fatalf("files %+v", f)
	}
	s := ParseOpenUI(`root = Timeline([Step("扫描", "312 个", "complete"), {title: "去重", state: "doing"}, Step("清理")])`)
	if s == nil || s.Kind != "steps" || len(s.Items) != 3 ||
		s.Items[0].Variant != "done" || s.Items[1].Variant != "active" || s.Items[2].Variant != "pending" {
		t.Fatalf("steps %+v", s)
	}
	st := ParseOpenUI(`root = StatCards([StatCard("已用", "412 GB", "+12 GB", "warn"), {label: "文件", value: 1200}, StatCard("空")])`)
	if st == nil || st.Kind != "stats" || len(st.Items) != 2 || st.Items[0].Tone != "warning" || st.Items[1].Text != "1200" {
		t.Fatalf("stats %+v", st)
	}
	g := ParseOpenUI(`root = Gallery(["/p/1.jpg", MediaItem("/p/2.mp4", "开场"), {src: "/p/3.webp"}], "相册")`)
	if g == nil || g.Kind != "gallery" || len(g.Items) != 3 || g.Items[1].Variant != "video" || g.Items[1].Text != "开场" {
		t.Fatalf("gallery %+v", g)
	}
}

func TestOpenGalleryCols(t *testing.T) {
	for src, want := range map[string]int{
		`root = Gallery(["/a.jpg"], "相册", 3)`:         3,
		`root = Gallery(["/a.jpg"], {cols: 4})`:       4,
		`root = Gallery(["/a.jpg"], "t", {cols: 99})`: OpenGalleryMaxCols,
		`root = Gallery(["/a.jpg"], "t")`:             0,
	} {
		v := ParseOpenUI(src)
		if v == nil || v.Kind != "gallery" || v.Extra != want {
			t.Fatalf("%s: %+v", src, v)
		}
	}
}

func TestOpenTaskProgress(t *testing.T) {
	v := ParseOpenUI(`root = TaskProgress("解压 a.zip", "42%", "8 MB/s", "x.pdf", "running")`)
	if v == nil || v.Kind != "taskprogress" || v.Min != 0.42 || v.Variant != "active" || v.Sub != "x.pdf" {
		t.Fatalf("%+v", v)
	}
	v = ParseOpenUI(`root = ArchiveProgress({title: "压缩", status: "failed", error: "磁盘已满"})`)
	if v == nil || v.Min != -1 || v.Variant != "error" || v.Sub != "磁盘已满" {
		t.Fatalf("%+v", v)
	}
}
