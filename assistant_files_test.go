package main

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	assistant "sunvale/internal/assistant"
	"sunvale/internal/fileview"
)

// assistant_files_test.go covers the reader and the picture path into the
// model: markers in tool text, the bag that holds the bytes and the rewrite
// that turns them into image parts. The stub model records what it is asked
// so the assertions are about the request, not about a live API.

func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 24, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 24; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 10), uint8(y * 20), 200, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadAnyFileCarriesPictures(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	writeTestPNG(t, path)

	bag := newMediaBag()
	block := readAnyFile(path, bag)
	if !strings.Contains(block, assistant.MediaOpen) || !strings.Contains(block, "原图") {
		t.Fatalf("block without marker: %s", block)
	}
	ids := assistant.MediaIDs(block)
	if len(ids) != 1 {
		t.Fatalf("ids = %v", ids)
	}
	frames := bag.take(ids)
	// The picture is opaque, so it ships as JPEG: that is what keeps four
	// frames cheap in a request.
	if len(frames) != 1 || frames[0].MIME != "image/jpeg" || len(frames[0].Data) == 0 {
		t.Fatalf("frames = %d mime=%s bytes=%d", len(frames), frames[0].MIME, len(frames[0].Data))
	}
	if shown := assistant.ToolText(block); strings.Contains(shown, assistant.MediaOpen) {
		t.Fatalf("marker leaked into display text: %s", shown)
	}

	// Text files stay text: no marker, no pictures.
	note := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(note, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if block := readAnyFile(note, bag); strings.Contains(block, assistant.MediaOpen) {
		t.Fatalf("text file produced pictures: %s", block)
	}
}

func TestSpecifiedFileNoteFrames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shot.png")
	writeTestPNG(t, path)
	note, frames := specifiedFileNote([]string{path})
	if !strings.Contains(note, "shot.png") || len(frames) != 1 {
		t.Fatalf("note %q frames %d", note, len(frames))
	}

	text := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(text, []byte("plain"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, frames := specifiedFileNote([]string{text}); len(frames) != 0 {
		t.Fatalf("text attachment produced %d frames", len(frames))
	}
}

// stubModel is a chat model that records every request and answers from a
// script of errors and replies.
type stubModel struct {
	calls  [][]*schema.Message
	errs   []error
	reason string
}

func (s *stubModel) Generate(_ context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	s.calls = append(s.calls, in)
	if len(s.errs) > 0 {
		err := s.errs[0]
		s.errs = s.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	return schema.AssistantMessage("ok", nil), nil
}

func (s *stubModel) Stream(_ context.Context, in []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	msg, err := s.Generate(context.Background(), in)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{msg}), nil
}

func imagePartCount(msgs []*schema.Message) int {
	n := 0
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for _, p := range m.UserInputMultiContent {
			if p.Type == schema.ChatMessagePartTypeImageURL {
				n++
			}
		}
	}
	return n
}

func TestVisionRewriteAttachesImages(t *testing.T) {
	bag := newMediaBag()
	ids := bag.add([]fileview.Frame{{Label: "原图", MIME: "image/jpeg", Data: []byte{0xff, 0xd8, 0xff}}})
	if len(ids) != 1 {
		t.Fatal("frame not stored")
	}
	body := "图片 shot.jpg\n附图 1 张，看图回答：" + assistant.MediaMarker(ids)
	in := []*schema.Message{
		schema.SystemMessage("sys"),
		schema.UserMessage("看这张图"),
		{Role: schema.Tool, Content: body, ToolCallID: "t1", ToolName: "read_files"},
	}

	// A model that sees: one synthetic user turn with the picture.
	stub := &stubModel{}
	vm := visionModel{inner: stub, bag: bag, vision: true}
	if _, err := vm.Generate(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	got := stub.calls[0]
	if imagePartCount(got) != 1 {
		t.Fatalf("image parts = %d in %+v", imagePartCount(got), got)
	}
	if strings.Contains(got[2].Content, assistant.MediaOpen) {
		t.Fatalf("marker kept in request: %q", got[2].Content)
	}
	if got[3] == nil || got[3].Role != schema.User || got[3].Content != "" {
		t.Fatalf("synthetic message = %+v", got[3])
	}

	// A model that does not see: nothing attached, and the tool text says so.
	stub = &stubModel{}
	vm = visionModel{inner: stub, bag: bag, vision: false}
	if _, err := vm.Generate(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if imagePartCount(stub.calls[0]) != 0 || len(stub.calls[0]) != 3 {
		t.Fatalf("blind request = %+v", stub.calls[0])
	}
	if !strings.Contains(stub.calls[0][2].Content, "图片已省略") {
		t.Fatalf("blind tool text = %q", stub.calls[0][2].Content)
	}
}

func TestVisionRetryWithoutImages(t *testing.T) {
	bag := newMediaBag()
	ids := bag.add([]fileview.Frame{{Label: "原图", MIME: "image/png", Data: []byte{0x89, 'P'}}})
	body := "附图：" + assistant.MediaMarker(ids)
	in := []*schema.Message{{Role: schema.Tool, Content: body, ToolCallID: "t1", ToolName: "read_files"}}

	stub := &stubModel{errs: []error{
		errString("400 invalid content type: image_url not supported"),
		nil,
	}}
	vm := visionModel{inner: stub, bag: bag, vision: true}
	if _, err := vm.Generate(context.Background(), in); err != nil {
		t.Fatalf("expected the text retry to succeed: %v", err)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("calls = %d", len(stub.calls))
	}
	if imagePartCount(stub.calls[1]) != 0 {
		t.Fatalf("retry still carried pictures: %+v", stub.calls[1])
	}

	// A failure that has nothing to do with pictures is not retried.
	stub = &stubModel{errs: []error{errString("401 invalid api key")}}
	vm = visionModel{inner: stub, bag: bag, vision: true}
	if _, err := vm.Generate(context.Background(), in); err == nil {
		t.Fatal("expected the auth error to surface")
	}
	if len(stub.calls) != 1 {
		t.Fatalf("auth failure retried %d times", len(stub.calls))
	}
}
