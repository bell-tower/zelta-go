package report

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/execx"
)

var ctx = context.Background()

func listCmd(ds string) string {
	return "zfs list -t filesystem,volume -Hpr -o name,snapshots_changed,usedbysnapshots -S snapshots_changed '" + ds + "'"
}

func snapCmd(ds string) string { return "zfs list -Hro name -t snapshot -d 1 '" + ds + "'" }

var (
	now    = app.TestNow.Unix()
	fresh  = fmt.Sprint(now - 3600)
	stale  = fmt.Sprint(now - 2*86400)
	report = execx.Out(
		"tank/Backups/current\t"+stale+"\t23805808640",
		"tank/Backups/default\t"+stale+"\t0",
		"tank/Backups/empty\t"+stale+"\t0",
		"tank/Backups/new\t"+fresh+"\t100",
		"tank/Backups/new\t"+fresh+"\t100",
		"tank/Backups\t-\t0",
		"tank/Backups/nosnaps\t-\t0",
		"short\trow",
	)
)

func TestStatuses(t *testing.T) {
	tr := app.NewTest("report", nil)
	tr.Fake.On(listCmd("tank/Backups"), report)
	tr.Fake.On(snapCmd("tank/Backups/default"), execx.Out())
	tr.Fake.On(snapCmd("tank/Backups/empty"), execx.Out("tank/Backups/empty@old"))
	if err := Run(ctx, tr.Runtime, []string{"tank/Backups"}); err != nil {
		t.Fatal(err)
	}
	if got := tr.Out.String(); got != "tank/Backups some snapshots are out of date: current empty\n" {
		t.Errorf("out = %q", got)
	}

	tests := []struct {
		rows execx.Result
		want string
	}{
		{execx.Out(), "tank/Backups no snapshots found"},
		{execx.Out("tank/Backups\t" + fresh + "\t1"), "tank/Backups snapshots are up to date"},
		{execx.Out("tank/Backups/a\t"+stale+"\t1", "tank/Backups\t"+stale+"\t5"), "tank/Backups all snapshots are out of date"},
	}
	for _, tt := range tests {
		tr := app.NewTest("report", nil)
		tr.Fake.On(listCmd("tank/Backups"), tt.rows)
		Run(ctx, tr.Runtime, []string{"tank/Backups"})
		if got := strings.TrimSpace(tr.Out.String()); got != tt.want {
			t.Errorf("got %q want %q", got, tt.want)
		}
	}
}

func TestTemplatesAndCommands(t *testing.T) {
	tr := app.NewTest("report", map[string]string{
		"BACKUP_ROOT":            "bk@vault:tank/Backups",
		"REPORT_MESSAGE_SOME":    "{status}|{endpoint}|{host}|{dataset}|{dslist}|{oldcount}|{okcount}|{message}",
		"REPORT_COMMAND_DEFAULT": "notify '{message}' {status}",
	})
	tr.Fake.OnPrefix("ssh -n bk@vault zfs list -t", report.WithStderr("cannot open"))
	tr.Fake.OnPrefix("ssh -n bk@vault zfs list -Hro", execx.Out())
	tr.Fake.OnPrefix("notify", execx.Out("ignored").WithStderr("notify failed"))
	if err := Run(ctx, tr.Runtime, []string{"ignored/operand"}); err != nil {
		t.Fatal(err)
	}
	want := "some|bk@vault:tank/Backups|vault|tank/Backups|current|1|3|"
	if got := strings.TrimSpace(tr.Out.String()); got != want {
		t.Errorf("message = %q want %q", got, want)
	}
	notify := tr.Fake.CallsMatching("notify")
	if len(notify) != 1 || notify[0] != "notify '"+want+"' some" {
		t.Errorf("command = %v", notify)
	}
	if !strings.Contains(tr.RawErr.String(), "cannot open") || !strings.Contains(tr.RawErr.String(), "notify failed") {
		t.Errorf("stderr = %q", tr.RawErr.String())
	}

	tr = app.NewTest("report", map[string]string{"REPORT_MESSAGE_DEFAULT": "[{status}] {okcount}", "REPORT_COMMAND_NONE": "alert {endpoint}"})
	tr.Fake.On(listCmd("a"), execx.Out())
	tr.Fake.On(listCmd("b"), execx.Out("b\t"+fresh+"\t0"))
	tr.Fake.On("alert a", execx.Out())
	Run(ctx, tr.Runtime, []string{"a", "b"})
	if got := tr.Out.String(); got != "[none] 0\n[ok] 1\n" {
		t.Errorf("default template = %q", got)
	}
	if len(tr.Fake.CallsMatching("alert")) != 1 {
		t.Errorf("status-specific command = %v", tr.Fake.Calls)
	}
}

func TestNoEndpoints(t *testing.T) {
	tr := app.NewTest("report", nil)
	err := Run(ctx, tr.Runtime, nil)
	if app.Code(err) != 1 || err.Error() != "BACKUP_ROOT not set and no endpoints provided" {
		t.Errorf("err = %v", err)
	}
}
