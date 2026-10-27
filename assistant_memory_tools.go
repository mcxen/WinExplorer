package main

// assistant_memory_tools.go is the MCP remember/forget/memory_list tools and
// the small plan/memory binders on sunvaleTools. Split from
// assistant_memory.go for maintainability (LAY2).

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (s *sunvaleTools) hasPlan(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plans == nil {
		return false
	}
	_, ok := s.plans[id]
	return ok
}

func (s *sunvaleTools) bindMemory(items []aiMemory) {
	s.mu.Lock()
	s.memory = append([]aiMemory(nil), items...)
	s.memAdd = nil
	s.memDrop = nil
	s.mu.Unlock()
}

func (s *sunvaleTools) drainMemory() (add []aiMemory, drop []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	add, drop = s.memAdd, s.memDrop
	s.memAdd, s.memDrop = nil, nil
	return add, drop
}

type memoryArgs struct {
	Text string `json:"text" jsonschema:"一句要记住或忘掉的话，不要包含密码或密钥"`
}

func (s *sunvaleTools) remember(_ context.Context, _ *mcp.CallToolRequest, args memoryArgs) (*mcp.CallToolResult, any, error) {
	text, err := cleanMemory(args.Text)
	if err != nil {
		return mcpErr(err), nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.memory {
		if strings.EqualFold(m.Text, text) {
			return mcpText("已经记得：" + m.Text), nil, nil
		}
	}
	item := aiMemory{ID: newID(), Text: text, Updated: time.Now()}
	s.memory = append(s.memory, item)
	s.memAdd = append(s.memAdd, item)
	for len(s.memory) > assistMaxMemory {
		old := s.memory[0]
		s.memory = s.memory[1:]
		fresh := false
		next := s.memAdd[:0]
		for _, m := range s.memAdd {
			if m.ID == old.ID {
				fresh = true
				continue
			}
			next = append(next, m)
		}
		s.memAdd = next
		if !fresh {
			s.memDrop = append(s.memDrop, old.ID)
		}
	}
	return mcpText("已记住：" + text), nil, nil
}

func (s *sunvaleTools) forget(_ context.Context, _ *mcp.CallToolRequest, args memoryArgs) (*mcp.CallToolResult, any, error) {
	q := strings.Join(strings.Fields(args.Text), " ")
	if q == "" {
		return mcpErr(fmt.Errorf("没有要忘掉的内容")), nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var hit []aiMemory
	ql := strings.ToLower(q)
	for _, m := range s.memory {
		if m.ID == q || strings.EqualFold(m.Text, q) || strings.Contains(strings.ToLower(m.Text), ql) {
			hit = append(hit, m)
		}
	}
	if len(hit) == 0 {
		return mcpErr(fmt.Errorf("没有这条记忆")), nil, nil
	}
	if len(hit) > 1 {
		var exact []aiMemory
		for _, m := range hit {
			if m.ID == q || strings.EqualFold(m.Text, q) {
				exact = append(exact, m)
			}
		}
		if len(exact) != 1 {
			return mcpErr(fmt.Errorf("有 %d 条相似的记忆，请说完整的一句", len(hit))), nil, nil
		}
		hit = exact
	}
	id := hit[0].ID
	next := s.memory[:0]
	for _, m := range s.memory {
		if m.ID != id {
			next = append(next, m)
		}
	}
	s.memory = next
	fresh := false
	added := s.memAdd[:0]
	for _, m := range s.memAdd {
		if m.ID == id {
			fresh = true
			continue
		}
		added = append(added, m)
	}
	s.memAdd = added
	if !fresh {
		s.memDrop = append(s.memDrop, id)
	}
	return mcpText("已忘掉：" + hit[0].Text), nil, nil
}

func (s *sunvaleTools) memoryList(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
	s.mu.Lock()
	items := append([]aiMemory(nil), s.memory...)
	s.mu.Unlock()
	if len(items) == 0 {
		return mcpText("无"), nil, nil
	}
	var b strings.Builder
	for _, m := range items {
		b.WriteString("- ")
		b.WriteString(m.Text)
		b.WriteByte('\n')
	}
	return mcpText(strings.TrimRight(b.String(), "\n")), nil, nil
}
