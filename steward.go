package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Steward model: agents propose, one steward writes, the machine owns setup facts.
//
// Every node carries a source and a status:
//   - source="system"  : written by the live-system refresher, authoritative
//   - source=<steward> : written by the designated steward (MEM_STEWARD_SOURCE)
//   - source="agent"   : written by anyone else
//
// Status gates what is visible:
//   - active   : trusted, returned by default
//   - proposed : inbox, waiting for the steward
//   - retired  : swept or rejected, kept for audit

const (
	statusActive   = "active"
	statusProposed = "proposed"
	statusRetired  = "retired"
	sourceSystem   = "system"

	// defaultStewardSource is the identity trusted to write active nodes when
	// MEM_STEWARD_SOURCE is unset.
	defaultStewardSource = "steward"
)

// stewardSources returns the identities allowed to write active nodes directly.
// MEM_STEWARD_SOURCE accepts a comma-separated list so the agent itself can be
// trusted alongside a separate curator identity.
func stewardSources() []string {
	raw := strings.TrimSpace(os.Getenv("MEM_STEWARD_SOURCE"))
	if raw == "" {
		raw = defaultStewardSource
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if s := strings.TrimSpace(part); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{defaultStewardSource}
	}
	return out
}

// stewardSource is the display form of the trusted identities.
func stewardSource() string {
	return strings.Join(stewardSources(), ",")
}

// isStewardIdentity reports whether this writer may bypass the inbox.
func isStewardIdentity(identity string) bool {
	for _, s := range stewardSources() {
		if identity == s {
			return true
		}
	}
	return false
}

// factRefreshInterval returns how often the live system is re-read.
func factRefreshInterval() time.Duration {
	m := 15
	if v := strings.TrimSpace(os.Getenv("MEM_FACTS_REFRESH_MINUTES")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			m = n
		}
	}
	return time.Duration(m) * time.Minute
}

// SystemFact is one fact read from the live machine.
type SystemFact struct {
	Key     string // stable identity, e.g. "system:service:memory.service"
	Body    string
	Topic   string
	Subject string
}

// collectSystemFacts reads the live system. Every command is best-effort:
// a missing tool or a permission error just means fewer facts, never a crash.
func collectSystemFacts() []SystemFact {
	var facts []SystemFact

	if out, err := runCmd("hostname"); err == nil {
		facts = append(facts, SystemFact{
			Key:     "system:host:hostname",
			Body:    fmt.Sprintf("Host hostname is %s.", strings.TrimSpace(out)),
			Topic:   "system",
			Subject: "hostname",
		})
	}
	if out, err := runCmd("uname", "-sr"); err == nil {
		facts = append(facts, SystemFact{
			Key:     "system:host:kernel",
			Body:    fmt.Sprintf("Host kernel is %s.", strings.TrimSpace(out)),
			Topic:   "system",
			Subject: "kernel",
		})
	}
	if out, err := runCmd("uname", "-m"); err == nil {
		facts = append(facts, SystemFact{
			Key:     "system:host:arch",
			Body:    fmt.Sprintf("Host architecture is %s.", strings.TrimSpace(out)),
			Topic:   "system",
			Subject: "arch",
		})
	}
	if out, err := runCmd("nproc"); err == nil {
		facts = append(facts, SystemFact{
			Key:     "system:host:cpus",
			Body:    fmt.Sprintf("Host has %s CPU cores.", strings.TrimSpace(out)),
			Topic:   "system",
			Subject: "cpus",
		})
	}
	if out, err := runCmd("uptime", "-p"); err == nil {
		facts = append(facts, SystemFact{
			Key:     "system:host:uptime",
			Body:    fmt.Sprintf("Host uptime: %s.", strings.TrimSpace(out)),
			Topic:   "system",
			Subject: "uptime",
		})
	}
	if out, err := runCmd("free", "-b"); err == nil {
		if total, avail, ok := parseFree(out); ok {
			facts = append(facts, SystemFact{
				Key:     "system:host:memory",
				Body:    fmt.Sprintf("Host has %s RAM total, %s available.", humanBytes(total), humanBytes(avail)),
				Topic:   "system",
				Subject: "memory",
			})
		}
	}
	if out, err := runCmd("df", "-B1", "--output=source,size,avail,target", "-x", "tmpfs", "-x", "devtmpfs"); err == nil {
		for _, line := range strings.Split(out, "\n")[1:] {
			f := strings.Fields(line)
			if len(f) < 4 {
				continue
			}
			size, err1 := strconv.ParseInt(f[1], 10, 64)
			avail, err2 := strconv.ParseInt(f[2], 10, 64)
			if err1 != nil || err2 != nil || size == 0 {
				continue
			}
			facts = append(facts, SystemFact{
				Key:     "system:disk:" + f[3],
				Body:    fmt.Sprintf("Filesystem %s mounted at %s has %s total, %s available.", f[0], f[3], humanBytes(size), humanBytes(avail)),
				Topic:   "system",
				Subject: "disk",
			})
		}
	}

	// Listening TCP ports. Ephemeral/high ports belong to short-lived client
	// sockets and change every refresh, so they are not durable facts.
	if out, err := runCmd("ss", "-tlnH"); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			f := strings.Fields(line)
			if len(f) < 4 {
				continue
			}
			addr := f[3]
			port := addr[strings.LastIndex(addr, ":")+1:]
			if port == "" || isEphemeralPort(port) {
				continue
			}
			facts = append(facts, SystemFact{
				Key:     "system:port:" + port,
				Body:    fmt.Sprintf("TCP port %s is listening on %s.", port, addr),
				Topic:   "network",
				Subject: "port " + port,
			})
		}
	}

	// systemd units that actually need attention. "inactive (dead)" and
	// "active (exited)" are the normal resting state for most units (oneshots,
	// disabled-on-boot helpers, socket-activated services), so only a failed
	// unit is a durable fact worth storing.
	sockets := socketActivatedUnits()
	if out, err := runCmd("systemctl", "list-units", "--type=service", "--all", "--no-pager", "--no-legend"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 4 {
				continue
			}
			unit, load, active, sub := f[0], f[1], f[2], f[3]
			if load != "loaded" || active != "failed" {
				continue
			}
			if socketTriggeredService(sockets, unit) {
				continue
			}
			facts = append(facts, SystemFact{
				Key:     "system:service:" + unit,
				Body:    fmt.Sprintf("Service %s failed (%s).", unit, sub),
				Topic:   "services",
				Subject: unit,
			})
		}
	}

	// User crontab.
	if out, err := runCmd("crontab", "-l"); err == nil {
		for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			facts = append(facts, SystemFact{
				Key:     fmt.Sprintf("system:cron:%d", i),
				Body:    fmt.Sprintf("Cron job %d: %s", i, line),
				Topic:   "schedule",
				Subject: "cron",
			})
		}
	}

	return facts
}

// isEphemeralPort reports whether port falls in the kernel's default ephemeral
// range. Those listeners are transient client-side sockets, not services.
func isEphemeralPort(port string) bool {
	n, err := strconv.Atoi(port)
	if err != nil {
		return true
	}
	return n >= 32768
}

// socketActivatedUnits returns the base names of service units that a live
// systemd socket can start on demand. A socket-activated unit is expected to be
// inactive while nobody is connecting, so reporting it as unhealthy is noise.
func socketActivatedUnits() map[string]bool {
	out, err := runCmd("systemctl", "list-units", "--type=socket", "--state=active", "--no-pager", "--no-legend")
	if err != nil {
		return nil
	}
	set := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || !strings.HasSuffix(f[0], ".socket") {
			continue
		}
		set[strings.TrimSuffix(f[0], ".socket")] = true
	}
	return set
}

// socketTriggeredService reports whether unit is one of those socket-activated
// services. Templated instances (foo@1.service) are matched on their base name.
func socketTriggeredService(sockets map[string]bool, unit string) bool {
	if len(sockets) == 0 {
		return false
	}
	name := strings.TrimSuffix(unit, ".service")
	if i := strings.Index(name, "@"); i >= 0 {
		name = name[:i]
	}
	return sockets[name]
}

func runCmd(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func parseFree(out string) (total, avail int64, ok bool) {
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "Mem:") {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 7 {
			return 0, 0, false
		}
		t, err1 := strconv.ParseInt(f[1], 10, 64)
		a, err2 := strconv.ParseInt(f[6], 10, 64)
		if err1 != nil || err2 != nil {
			return 0, 0, false
		}
		return t, a, true
	}
	return 0, 0, false
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

// refreshSystemFacts upserts every collected fact. Upsert is keyed on fact_key
// so repeated refreshes update in place instead of piling up duplicates.
// Facts that disappeared from the live system are retired, not deleted.
func refreshSystemFacts() (upserted, retired int, err error) {
	facts := collectSystemFacts()
	seen := make(map[string]bool, len(facts))
	nowStr := now()

	for _, f := range facts {
		seen[f.Key] = true
		_, err := pool.Exec(context.Background(), `
			INSERT INTO memory_nodes (id, body, hash, layer, fact_type, subject, confidence, metadata, topic, captured_at, created_at, updated_at, source, status, fact_key)
			VALUES ($1, $2, $3, 'semantic', 'fact', $4, 1.0, $5, $6, $7, $7, $7, $8, $9, $10)
			ON CONFLICT (fact_key) WHERE fact_key IS NOT NULL
			DO UPDATE SET body = EXCLUDED.body, hash = EXCLUDED.hash, subject = EXCLUDED.subject,
			              metadata = EXCLUDED.metadata, updated_at = EXCLUDED.updated_at,
			              source = 'system', status = 'active'`,
			uuidStr(), f.Body, hashBody(f.Body), f.Subject,
			fmt.Sprintf(`{"refreshed_at":%q,"writer":"system"}`, nowStr),
			f.Topic, nowStr, sourceSystem, statusActive, f.Key)
		if err != nil {
			return upserted, retired, err
		}
		upserted++
	}

	// Retire system facts that no longer exist on the machine.
	rows, err := pool.Query(context.Background(),
		`SELECT id, fact_key FROM memory_nodes WHERE source = 'system' AND status = 'active'`)
	if err != nil {
		return upserted, retired, err
	}
	var stale []string
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			continue
		}
		if !seen[key] {
			stale = append(stale, id)
		}
	}
	rows.Close()

	for _, id := range stale {
		_, err := pool.Exec(context.Background(),
			`UPDATE memory_nodes SET status = $1, updated_at = $2, metadata = jsonb_set(metadata::jsonb, '{retired_reason}', '"no longer present on the live system"')::text WHERE id = $3`,
			statusRetired, nowStr, id)
		if err != nil {
			return upserted, retired, err
		}
		retired++
	}
	return upserted, retired, nil
}

// setupFactPattern matches bodies that read like live-system setup facts:
// ports, services, paths, addresses, schedules.
var setupFactPattern = regexp.MustCompile(`(?i)(port\s+\d{2,5}|listening on|systemd|systemctl|/etc/|/home/|/var/|/opt/|\b\d{1,3}(\.\d{1,3}){3}\b|crontab|cron job|\.service\b|\.timer\b)`)

// sweepAgentSetupFacts demotes agent-written nodes that describe the live
// system. Nothing is deleted: nodes that overlap a real system fact are
// retired with a reason, the rest drop to the inbox for the steward.
func sweepAgentSetupFacts() (retired, demoted int, err error) {
	nowStr := now()
	rows, err := pool.Query(context.Background(),
		`SELECT id, body FROM memory_nodes WHERE source <> 'system' AND status = 'active'`)
	if err != nil {
		return 0, 0, err
	}
	type candidate struct{ id, body string }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.body); err != nil {
			continue
		}
		candidates = append(candidates, c)
	}
	rows.Close()

	for _, c := range candidates {
		if !setupFactPattern.MatchString(c.body) {
			continue
		}
		// Only act when a live system fact already covers this. Merely
		// resembling a setup fact is not evidence of staleness: real notes
		// contain paths, ports, and unit names all the time.
		var overlap int
		_ = pool.QueryRow(context.Background(), `
			SELECT count(*) FROM memory_nodes
			WHERE source = 'system' AND status = 'active'
			  AND fact_key IS NOT NULL
			  AND (subject = ANY(string_to_array(lower($1), ' ')) OR lower(body) LIKE '%' || lower($1) || '%')`,
			firstWords(c.body, 6)).Scan(&overlap)
		if overlap == 0 {
			continue
		}
		_, err := pool.Exec(context.Background(),
			`UPDATE memory_nodes SET status = $1, updated_at = $2,
			 metadata = jsonb_set(metadata::jsonb, '{swept_reason}', '"superseded by a live system fact"')::text
			 WHERE id = $3`,
			statusRetired, nowStr, c.id)
		if err != nil {
			return retired, demoted, err
		}
		retired++
	}
	return retired, demoted, nil
}

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) > n {
		f = f[:n]
	}
	return strings.Join(f, " ")
}

func uuidStr() string {
	return uuid.New().String()
}

// writerIdentity reports who is writing a node: the steward, or someone else.
// Declared with the X-Mem-Source header, falling back to the JSON "source"
// field. Unidentified writers are "agent", which is the safe default.
func writerIdentity(r *http.Request, data map[string]interface{}) string {
	if v := strings.TrimSpace(r.Header.Get("X-Mem-Source")); v != "" {
		return v
	}
	if data != nil {
		if v, ok := data["source"].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return "agent"
}

// statusForWriter gates writes by identity: the steward writes active nodes
// directly, everyone else lands in the inbox as proposed.
func statusForWriter(identity string) string {
	if isStewardIdentity(identity) {
		return statusActive
	}
	return statusProposed
}

// startFactRefresher runs the live-system refresh and the sweeper on a ticker.
func startFactRefresher(stop <-chan struct{}) {
	interval := factRefreshInterval()
	log.Printf("fact refresher: every %v (steward=%q)", interval, stewardSource())

	runOnce := func() {
		upserted, retired, err := refreshSystemFacts()
		if err != nil {
			log.Printf("fact refresher: refresh failed: %v", err)
		} else {
			log.Printf("fact refresher: %d facts upserted, %d retired", upserted, retired)
		}
		sr, sd, err := sweepAgentSetupFacts()
		if err != nil {
			log.Printf("fact refresher: sweep failed: %v", err)
		} else if sr+sd > 0 {
			log.Printf("fact sweeper: %d retired, %d demoted to inbox", sr, sd)
		}
	}

	runOnce()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			runOnce()
		case <-stop:
			return
		}
	}
}

// --- Steward HTTP surface ---

// handleInbox lists proposed nodes waiting for the steward.
func handleInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	rows, err := pool.Query(context.Background(),
		`SELECT id, body, layer, topic, subject, confidence, fact_type, metadata, captured_at, created_at, updated_at, source, status, fact_key
		 FROM memory_nodes WHERE status = $1 ORDER BY created_at DESC LIMIT 500`, statusProposed)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	nodes := []Node{}
	for rows.Next() {
		var n Node
		var captured, created, updated interface{}
		var factKey *string
		if err := rows.Scan(&n.ID, &n.Body, &n.Layer, &n.Topic, &n.Subject, &n.Confidence, &n.FactType, &n.Metadata, &captured, &created, &updated, &n.Source, &n.Status, &factKey); err != nil {
			continue
		}
		n.CapturedAt = fmt.Sprint(captured)
		n.CreatedAt = fmt.Sprint(created)
		n.UpdatedAt = fmt.Sprint(updated)
		if factKey != nil {
			n.FactKey = *factKey
		}
		nodes = append(nodes, n)
	}
	jsonResp(w, 200, map[string]interface{}{"nodes": nodes, "count": len(nodes)})
}

// handleNodeDecision promotes or rejects a proposed node. Steward only: enforced
// by requireSteward, not just by convention.
func handleNodeDecision(w http.ResponseWriter, r *http.Request, decision string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !requireSteward(w, r) {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/v1/memory/nodes/")
	id = strings.TrimSuffix(id, "/promote")
	id = strings.TrimSuffix(id, "/reject")
	id = strings.TrimPrefix(id, "/api/nodes/")
	if id == "" {
		jsonResp(w, 400, map[string]string{"error": "missing id"})
		return
	}

	// Refuse to touch machine-owned facts.
	var src string
	if err := pool.QueryRow(context.Background(), `SELECT source FROM memory_nodes WHERE id = $1`, id).Scan(&src); err != nil {
		jsonResp(w, 404, map[string]string{"error": "not found"})
		return
	}
	if src == sourceSystem {
		jsonResp(w, http.StatusForbidden, map[string]string{"error": "system facts are owned by the live-system refresher and cannot be promoted or rejected"})
		return
	}

	newStatus := statusActive
	reason := "promoted by steward"
	if decision == "reject" {
		newStatus = statusRetired
		reason = "rejected by steward"
	}
	_, err := pool.Exec(context.Background(),
		`UPDATE memory_nodes SET status = $1, updated_at = $2,
		 metadata = jsonb_set(metadata::jsonb, '{steward_decision}', $3::jsonb)::text
		 WHERE id = $4`,
		newStatus, now(), fmt.Sprintf(`{"decision":%q,"at":%q}`, decision, now()), id)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, 200, map[string]string{"status": newStatus, "id": id, "reason": reason})
}

// handleBrief returns the compact setup brief: every active system fact in one
// small payload, meant to be loaded at the start of a session.
func handleBrief(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		jsonResp(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	rows, err := pool.Query(context.Background(),
		`SELECT fact_key, topic, subject, body FROM memory_nodes
		 WHERE source = 'system' AND status = 'active' AND fact_key IS NOT NULL
		 ORDER BY topic, fact_key`)
	if err != nil {
		jsonResp(w, 500, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type briefItem struct {
		Key     string `json:"key"`
		Topic   string `json:"topic"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
	}
	items := []briefItem{}
	for rows.Next() {
		var b briefItem
		if err := rows.Scan(&b.Key, &b.Topic, &b.Subject, &b.Body); err != nil {
			continue
		}
		items = append(items, b)
	}
	jsonResp(w, 200, map[string]interface{}{
		"brief":  items,
		"count":  len(items),
		"source": "live system",
	})
}

// marshalFactKeys is used by tests.
func marshalFactKeys(facts []SystemFact) string {
	keys := make([]string, len(facts))
	for i, f := range facts {
		keys[i] = f.Key
	}
	b, _ := json.Marshal(keys)
	return string(b)
}
