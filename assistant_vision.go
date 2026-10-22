package main

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	assistant "sunvale/internal/assistant"
	"sunvale/internal/fileview"
)

// assistant_vision.go gets pictures in front of a model that can see them.
// Two sources need the same treatment: files the model read through the MCP
// tools (their bytes live in the run's media bag and the tool text only
// carries ids) and files the user attached to the send. Both end up as real
// image parts in the request, and both are dropped again — silently, as
// words — when the model turns out not to accept pictures.

// visionModel wraps the run's chat model. The wrapper rewrites the message
// list on its way out and never touches the agent's own state, so the
// transcript and the tool history stay text.
type visionModel struct {
	inner  model.BaseChatModel
	bag    *assistMediaBag
	vision bool
}

var _ model.BaseChatModel = visionModel{}

func (m visionModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	out, attached := m.rewrite(in)
	msg, err := m.inner.Generate(ctx, out, opts...)
	if err != nil && attached && visionRejected(err) {
		return m.inner.Generate(ctx, m.withoutImages(in), opts...)
	}
	return msg, err
}

func (m visionModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	out, attached := m.rewrite(in)
	stream, err := m.inner.Stream(ctx, out, opts...)
	if err != nil && attached && visionRejected(err) {
		return m.inner.Stream(ctx, m.withoutImages(in), opts...)
	}
	return stream, err
}

// WithTools forwards tool binding to the wrapped model when it supports the
// immutable form, so the agent can still give the model its tool list.
func (m visionModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	if tc, ok := m.inner.(model.ToolCallingChatModel); ok {
		inner, err := tc.WithTools(tools)
		if err != nil {
			return nil, err
		}
		return visionModel{inner: inner, bag: m.bag, vision: m.vision}, nil
	}
	return nil, errors.New("模型不支持工具绑定")
}

// rewrite strips the media markers and appends one synthetic user message
// per tool result that carried pictures. The synthetic message is the only
// place images go: the chat-completions API accepts pictures in user turns,
// not in tool results.
func (m visionModel) rewrite(in []*schema.Message) ([]*schema.Message, bool) {
	attached := false
	out := make([]*schema.Message, 0, len(in)+2)
	for _, msg := range in {
		if msg == nil || !strings.Contains(msg.Content, assistant.MediaOpen) {
			out = append(out, msg)
			continue
		}
		ids := assistant.MediaIDs(msg.Content)
		copy := *msg
		copy.Content = assistant.StripMedia(msg.Content)
		out = append(out, &copy)
		frames := m.bag.take(ids)
		if !m.vision || len(frames) == 0 {
			if len(frames) > 0 {
				// Say so instead of leaving a hole in the reasoning.
				copy.Content += "\n（这个模型不能看图，图片已省略：用文字描述文件即可）"
			}
			continue
		}
		out = append(out, mediaMessage(frames))
		attached = true
	}
	return out, attached
}

// withoutImages is the degraded form used when a model refuses pictures.
func (m visionModel) withoutImages(in []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(in))
	for _, msg := range in {
		if msg == nil || !strings.Contains(msg.Content, assistant.MediaOpen) {
			out = append(out, msg)
			continue
		}
		copy := *msg
		copy.Content = assistant.StripMedia(msg.Content) + "\n（模型不接受图片，这里以文字说明代替）"
		out = append(out, &copy)
	}
	return out
}

// mediaMessage is one synthetic user turn carrying pictures. Content stays
// empty: the request marshalling rejects a message that has both plain text
// and parts.
func mediaMessage(frames []fileview.Frame) *schema.Message {
	parts := imageParts("上面工具读到的画面在这里，请直接看图回答：", frames)
	return &schema.Message{Role: schema.User, UserInputMultiContent: parts}
}

// imageParts builds the parts of a multimodal user message: one text part
// and one image part per picture.
func imageParts(text string, frames []fileview.Frame) []schema.MessageInputPart {
	parts := make([]schema.MessageInputPart, 0, len(frames)+1)
	parts = append(parts, schema.MessageInputPart{Type: schema.ChatMessagePartTypeText, Text: text})
	for _, f := range frames {
		data := base64.StdEncoding.EncodeToString(f.Data)
		mime := f.MIME
		if mime == "" {
			mime = "image/jpeg"
		}
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{
				MessagePartCommon: schema.MessagePartCommon{
					Base64Data: &data,
					MIMEType:   mime,
				},
				Detail: schema.ImageURLDetailHigh,
			},
		})
	}
	return parts
}

// attachFiles adds the user's attached pictures to a user message. The text
// goes in as one part alongside them, because a multimodal message carries
// no plain Content field.
func attachFiles(text string, frames []fileview.Frame) *schema.Message {
	if len(frames) == 0 {
		return schema.UserMessage(text)
	}
	return &schema.Message{Role: schema.User, UserInputMultiContent: imageParts(text, frames)}
}

// visionRejected reports whether a failure is the model saying it does not
// take pictures. Anything else (bad key, quota, network) is a real failure
// and must not be retried as text.
func visionRejected(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, mark := range []string{
		"image", "vision", "multimodal", "image_url", "input_image",
		"unsupported content", "invalid content", "content type", "not supported",
		"does not support", "media type", "base64",
	} {
		if strings.Contains(msg, mark) {
			return true
		}
	}
	return false
}
