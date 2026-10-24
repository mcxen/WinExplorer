package assistant

import "testing"

func TestParseOpenUIMediaList(t *testing.T) {
	v := ParseOpenUI(`root = MediaList([m1, m2, m3, bad], "步骤", "numbered")
m1 = MediaItem("/p/a.PNG", "封面", "第二张")
m2 = MediaItem("/p/clip.mov?x", "开场", "前 5 秒", "movie")
m3 = {src: "https://h/s.pdf?t=1", title: "脚本", body: "第 3 页", n: "9"}
bad = MediaItem("")`)
	if v == nil || v.Kind != "medialist" || v.Text != "步骤" || !v.Row || len(v.Items) != 3 {
		t.Fatalf("list %+v", v)
	}
	want := [][3]string{{"image", "封面", "1"}, {"video", "开场", "2"}, {"doc", "脚本", "9"}}
	for i, w := range want {
		it := v.Items[i]
		if it.Variant != w[0] || it.Text != w[1] || OpenMediaNumber(v, it, i) != w[2] {
			t.Fatalf("item %d %+v", i, it)
		}
	}
	if v.Items[2].Path != "https://h/s.pdf?t=1" || v.Items[2].Sub != "第 3 页" {
		t.Fatalf("object item %+v", v.Items[2])
	}
	if OpenMediaKind("", "https://h/a.jpg?w=1") != "image" {
		t.Fatal("url kind")
	}
	plain := ParseOpenUI(`root = MediaList([MediaItem("/a.txt", "说明", "x")])`)
	if plain == nil || OpenMediaNumber(plain, plain.Items[0], 0) != "" {
		t.Fatalf("unnumbered %+v", plain)
	}
}
