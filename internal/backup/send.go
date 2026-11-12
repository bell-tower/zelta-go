package backup

import (
	"regexp"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/shellwords"
	"github.com/bell-tower/zelta-go/internal/zfs"
)

// sendCheckActive reports whether unsupported default send options may be
// detected and dropped.
func (r *run) sendCheckActive() bool {
	return r.opts.Bool("SEND_CHECK") && !r.opts.Has("SEND_OVERRIDE")
}

var (
	sendOptionErrorRe = regexp.MustCompile(`(invalid|illegal|unknown|unrecognized) option|usage: zfs send`)
	quotedOptionRe    = regexp.MustCompile(`(invalid|illegal|unknown|unrecognized) option '([^']*)'`)
	dashOptionRe      = regexp.MustCompile(`(invalid|illegal|unknown|unrecognized) option -- (.)`)
)

func sendOptionError(line string) bool { return sendOptionErrorRe.MatchString(line) }

// optionErrorMatchesFeature reports whether a zfs send option error could
// refer to feature (e.g. "-e"). Errors that do not name an option match any
// feature.
func optionErrorMatchesFeature(line, feature string) bool {
	if m := quotedOptionRe.FindStringSubmatch(line); m != nil {
		return m[2] == "" || m[2] == feature || m[2] == feature[1:2]
	}
	if m := dashOptionRe.FindStringSubmatch(line); m != nil {
		return m[2] == feature[1:2]
	}
	return true
}

func featureLongOpt(feature, opt string) bool {
	switch feature {
	case "-e":
		return opt == "--embed" || opt == "--embedded-data"
	case "-c":
		return opt == "--compressed"
	case "-L":
		return opt == "--largeblock" || opt == "--large-block"
	}
	return false
}

func flagsHaveFeature(flags []string, feature string) bool {
	short := feature[1:2]
	for _, t := range flags {
		if t == feature || featureLongOpt(feature, t) {
			return true
		}
		if strings.HasPrefix(t, "-") && !strings.HasPrefix(t, "--") && strings.Contains(t[1:], short) {
			return true
		}
	}
	return false
}

func removeFeature(flags []string, feature string) []string {
	short := feature[1:2]
	var out []string
	for _, t := range flags {
		if t == feature || featureLongOpt(feature, t) {
			continue
		}
		if strings.HasPrefix(t, "-") && !strings.HasPrefix(t, "--") {
			t = "-" + strings.ReplaceAll(t[1:], short, "")
			if t == "-" {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

var sendCheckFeatures = []string{"-e", "-c", "-L"}

func (r *run) applySendCheck(flags []string) []string {
	if !r.sendCheckActive() {
		return flags
	}
	for _, f := range sendCheckFeatures {
		if r.sendCheckDisabled[f] {
			flags = removeFeature(flags, f)
		}
	}
	return flags
}

// sendFlags returns the zfs send options for a dataset.
func (r *run) sendFlags(suffix string) []string {
	if tok := r.dataset(tgt, suffix).prop("receive_resume_token"); tok != "" {
		return []string{"-t", tok}
	}
	if v := r.opts.Get("SEND_OVERRIDE"); v != "" {
		return shellwords.MustSplit(v)
	}
	sd := r.dataset(src, suffix)
	a := r.action(suffix)
	var flags string
	switch {
	case r.verb == "replicate":
		flags = r.opts.Get("SEND_REPLICATE")
	case a.sendDecrypted:
		if !a.warnedDecrypted {
			a.warnedDecrypted = true
			r.rt.Log.Warnf("raw incremental unavailable at %s; falling back to decrypted send: %s", r.pair(suffix).match, r.src.DS+suffix)
		}
		flags = r.opts.Get("SEND_DECRYPTED")
	case truthy(sd.prop("encryption")):
		flags = r.opts.Get("SEND_RAW")
	default:
		flags = r.opts.Get("SEND_DEFAULT")
	}
	if r.sendCheckActive() && r.verb != "replicate" && !a.sendDecrypted && !truthy(sd.prop("encryption")) && strings.TrimSpace(flags) == "--raw" {
		flags = "-L -c -e"
	}
	return r.applySendCheck(shellwords.MustSplit(flags))
}

// sendIncremental returns the "-I/-i snapshot" arguments, if any.
func (r *run) sendIncremental(suffix string) []command.Arg {
	p := r.pair(suffix)
	var from string
	switch {
	case p.sourceOriginMatch != "":
		from = p.sourceOriginMatch
	case r.dataset(tgt, suffix).prop("receive_resume_token") != "":
		return nil
	case p.match != "":
		from = r.src.DS + suffix + p.sourceStart
	default:
		return nil
	}
	flag := "-i"
	if r.opts.Bool("SEND_INTR") && !r.action(suffix).forceIncrement {
		flag = "-I"
	}
	return []command.Arg{command.Word(flag), command.Name(from)}
}

func (r *run) sendArgs(suffix, mode string) []command.Arg {
	args := command.Words("send", mode)
	args = append(args, command.Words(r.sendFlags(suffix)...)...)
	args = append(args, r.sendIncremental(suffix)...)
	if r.dataset(tgt, suffix).prop("receive_resume_token") == "" {
		args = append(args, command.Name(r.src.DS+suffix+r.pair(suffix).sourceEnd))
	}
	return args
}

// sendCommand returns "zfs send -P ...", reached through the source's remote
// shell when remote is set and the source is remote.
func (r *run) sendCommand(suffix string, remote bool) command.Cmd {
	return r.zfsOn(r.src, remote, zfs.RemoteSend, r.sendArgs(suffix, "-P")...)
}

func (r *run) zfsOn(ep endpoint.Endpoint, remote bool, kind string, args ...command.Arg) command.Cmd {
	if remote {
		return zfs.Command(r.rt, ep, kind, args...)
	}
	return zfs.Local(args...)
}

// runSendCheck probes "zfs send -nv" for the top dataset and drops default
// options the source rejects.
func (r *run) runSendCheck() {
	if !r.sendCheckActive() || r.dryRun() || !r.action("").canSync {
		return
	}
	for {
		cmd := r.zfsOn(r.src, true, zfs.RemoteSend, r.sendArgs("", "-nv")...)
		res := r.rt.Run(r.ctx, cmd)
		var errMsg string
		for _, l := range res.Lines {
			line := app.SanitizeLine(l.Text)
			if sendOptionError(line) {
				errMsg = line
			} else if zfs.ConnectionError.MatchString(line) {
				r.rt.Log.Warnf("%s", line)
			}
		}
		if errMsg == "" || !r.dropSendFeature("", errMsg) {
			return
		}
	}
}

// dropSendFeature disables the next default send feature that an option
// error could refer to, reporting whether one was dropped.
func (r *run) dropSendFeature(suffix, errMsg string) bool {
	if !r.sendCheckActive() || !sendOptionError(errMsg) {
		return false
	}
	flags := r.sendFlags(suffix)
	for _, f := range sendCheckFeatures {
		if r.sendCheckDisabled[f] || !optionErrorMatchesFeature(errMsg, f) || !flagsHaveFeature(flags, f) {
			continue
		}
		r.sendCheckDisabled[f] = true
		r.rt.Log.Warnf("send-check dropping unsupported zfs send option: %s", f)
		return true
	}
	return false
}

// recvFlags returns the zfs receive options for a dataset.
func (r *run) recvFlags(suffix string) []command.Arg {
	if v := r.opts.Get("RECV_OVERRIDE"); v != "" {
		return command.Fragment(v)
	}
	var args []command.Arg
	add := func(opt string) { args = append(args, command.Fragment(r.opts.Get(opt))...) }
	add("RECV_DEFAULT")
	p := r.pair(suffix)
	if p.match == "" || p.targetOrigin != "" {
		if suffix == "" {
			add("RECV_TOP")
		}
		switch r.dataset(src, suffix).prop("type") {
		case "volume":
			add("RECV_VOL")
		case "filesystem":
			add("RECV_FS")
		}
	}
	for _, prop := range r.recvPropsAdd {
		args = append(args, command.Word("-o"), command.Word(prop))
	}
	for _, prop := range r.recvPropsDel {
		args = append(args, command.Word("-x"), command.Word(prop))
	}
	if r.opts.Bool("RESUME") {
		add("RECV_PARTIAL")
	}
	if p.targetOrigin != "" {
		args = append(args, command.Word("-o"), command.LitName("origin=", p.targetOrigin+suffix+p.match))
	}
	return args
}

// recvCommand returns "zfs recv -v ...", reached through the target's remote
// shell when remote is set and the target is remote.
func (r *run) recvCommand(suffix string, remote bool) command.Cmd {
	args := append(command.Words("recv", "-v"), r.recvFlags(suffix)...)
	return r.zfsOn(r.tgt, remote, zfs.RemoteRecv, append(args, command.Name(r.tgt.DS+suffix))...)
}

func (r *run) pipeStage() []command.Cmd {
	if r.receivePipe == "" {
		return nil
	}
	return []command.Cmd{command.Script(r.receivePipe + " 2>>/dev/tty")}
}

// syncPipeline returns the send/receive pipeline for a dataset. With both
// endpoints on one host the pipeline runs there; with two remote hosts the
// target pulls (default) or the source pushes; otherwise the stream passes
// through the local host.
func (r *run) syncPipeline(suffix string) command.Pipeline {
	orchestrate := r.src.IsRemote() && r.tgt.IsRemote()
	direction := strings.ToUpper(r.opts.Get("SYNC_DIRECTION"))
	switch {
	case r.src.Remote == r.tgt.Remote:
		send, recv := r.sendCommand(suffix, false), r.recvCommand(suffix, false)
		if !r.src.IsRemote() {
			return append(append(command.Pipeline{send}, r.pipeStage()...), recv)
		}
		script := "{ " + send.String() + "|" + recv.String() + " ; }"
		return command.Pipeline{{Remote: r.rt.Remote(r.src, zfs.RemoteDefault), Script: script}}
	case orchestrate && direction == "PULL":
		script := r.sendCommand(suffix, true).String() + " | " + r.recvCommand(suffix, false).String()
		return command.Pipeline{{Remote: r.rt.Remote(r.tgt, zfs.RemoteDefault), Script: script}}
	case orchestrate && direction == "PUSH":
		script := r.sendCommand(suffix, false).String() + " | " + r.recvCommand(suffix, true).String()
		return command.Pipeline{{Remote: r.rt.Remote(r.src, zfs.RemoteDefault), Script: script}}
	}
	if orchestrate && !r.tree.warnedProxy {
		r.tree.warnedProxy = true
		r.rt.Log.Warnf("syncing remote endpoints through localhost; consider --push or --pull")
	}
	return append(append(command.Pipeline{r.sendCommand(suffix, true)}, r.pipeStage()...), r.recvCommand(suffix, true))
}
