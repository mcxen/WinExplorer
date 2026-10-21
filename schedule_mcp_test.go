package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"sunvale/internal/schedule"
)

func mcpTextOf(r *mcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if t, ok := r.Content[0].(*mcp.TextContent); ok {
		return t.Text
	}
	return ""
}

func useTempScheduleStore(t *testing.T) *schedule.Store {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "schedules.json")
	st, err := schedule.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = ensureScheduleStore()
	schedStore = st
	return st
}

func TestScheduleToolsCRUD(t *testing.T) {
	st := useTempScheduleStore(t)
	src := filepath.Join(t.TempDir(), "Downloads")
	dst := filepath.Join(t.TempDir(), "Documents")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	tools := sunvaleToolState
	res, _, err := tools.scheduleCreate(context.Background(), nil, scheduleCreateArgs{
		Name: "每晚整理下载文档", Time: "19:00", Days: "每天",
		Action: "file_rule", Source: src, Dest: dst,
		Match: ".pdf,.docx", Op: "move",
	})
	if err != nil {
		t.Fatal(err)
	}
	out := mcpTextOf(res)
	if res.IsError || !strings.Contains(out, "已创建") {
		t.Fatalf("create: %s", out)
	}
	jobs := st.Jobs()
	if len(jobs) != 1 || jobs[0].Hour != 19 || jobs[0].Op != schedule.OpMove {
		t.Fatalf("job=%+v", jobs)
	}
	if jobs[0].NextRunAt.IsZero() {
		t.Fatal("NextRunAt empty")
	}
	id := jobs[0].ID

	list, _, err := tools.scheduleList(context.Background(), nil, scheduleListArgs{})
	if err != nil {
		t.Fatal(err)
	}
	listed := mcpTextOf(list)
	if !strings.Contains(listed, id) || !strings.Contains(listed, "每晚整理下载文档") {
		t.Fatalf("list: %s", listed)
	}

	upd, _, err := tools.scheduleUpdate(context.Background(), nil, scheduleUpdateArgs{
		ID:                 id,
		scheduleCreateArgs: scheduleCreateArgs{Time: "20:30", Days: "工作日"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mcpTextOf(upd), "已更新") {
		t.Fatal(mcpTextOf(upd))
	}
	got, _ := st.Get(id)
	if got.Hour != 20 || got.Minute != 30 || len(got.Weekdays) != 5 {
		t.Fatalf("updated=%+v", got)
	}

	del, _, err := tools.scheduleDelete(context.Background(), nil, scheduleDeleteArgs{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mcpTextOf(del), "已删除") {
		t.Fatal(mcpTextOf(del))
	}
	if len(st.Jobs()) != 0 {
		t.Fatal("not deleted")
	}
}

func TestScheduleJobFromArgsBackup(t *testing.T) {
	j, err := scheduleJobFromArgs("", scheduleCreateArgs{
		Name: "备份文档", Time: "07:00", Action: "file_rule",
		Source: "/tmp/a", Dest: "/tmp/b", Op: "copy", DateStamp: true,
		Match: ".pdf",
	})
	if err != nil {
		t.Fatal(err)
	}
	if j.Op != schedule.OpCopy || !j.DateStamp || j.Cron != "0 7 * * *" {
		t.Fatalf("%+v", j)
	}
	_ = time.Now
}

func TestPromptListsScheduleTools(t *testing.T) {
	for _, name := range []string{
		"schedule_list", "schedule_create", "schedule_update", "schedule_delete", "schedule_run",
	} {
		if !strings.Contains(assistantInstruction, name) {
			t.Errorf("Instruction missing %s", name)
		}
		found := false
		for _, n := range sunvaleBuiltinToolNames() {
			if n == name {
				found = true
			}
		}
		if !found {
			t.Errorf("sunvaleBuiltinToolNames missing %s", name)
		}
	}
}
