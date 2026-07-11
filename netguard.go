package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	localIPScanInterval        = 200 * time.Millisecond
	localIPLogInterval         = 5 * time.Minute
	forceDisconnectDuration    = 120 * time.Minute
	forcePromptTimeout         = 30 * time.Second
	networkActionPromptTimeout = 3 * time.Second
	networkActionConfirmCache  = 30 * time.Minute
	networkActionDismissSkip   = 3 * time.Minute
	networkRecheckDelay        = 3 * time.Second
	networkCheckTimeout        = 8 * time.Second
	networkActionTimeout       = 30 * time.Second
	disconnectRetryCooldown    = 15 * time.Second
)

type networkGuard struct {
	mu                 sync.Mutex
	enabled            bool
	allowedCountries   []string
	providers          []string
	actions            []networkActionConfig
	forceTimes         []int
	providerCursor     int
	nextCheck          time.Time
	checking           bool
	skipUntil          time.Time
	forceUntil         time.Time
	forcePrompting     bool
	actionPrompting    bool
	actionConfirmUntil time.Time
	nextForceAttempt   time.Time
	lastForceSlot      string
	lastInfo           string
	lastIP             string
	lastLocalScan      time.Time
	lastLocalLog       time.Time
	lastLocalIPs       string
	localIPsKnown      bool
	forceDisconnect    bool
	lastDisconnect     time.Time
	actionRunning      bool
}

type publicIPLocation struct {
	IP          string
	CountryCode string
	Country     string
	City        string
	Source      string
}

type locationEndpoint struct {
	id   string
	name string
	url  string
}

var locationEndpoints = []locationEndpoint{
	{id: "ipinfo", name: "ipinfo.io", url: "https://ipinfo.io/json"},
	{id: "ifconfig", name: "ifconfig.co", url: "https://ifconfig.co/json"},
	{id: "ip-api", name: "ip-api.com", url: "http://ip-api.com/json/?fields=status,country,countryCode,city,query,message"},
	{id: "ipapi", name: "ipapi.co", url: "https://ipapi.co/json/"},
	{id: "ipwhois", name: "ipwho.is", url: "https://ipwho.is/"},
}

func newNetworkGuard(cfg networkCheckConfig) *networkGuard {
	g := &networkGuard{}
	g.configure(cfg)
	return g
}

func (g *networkGuard) configure(cfg networkCheckConfig) {
	allowed := normalizeCountryCodes(cfg.AllowedCountries)
	if len(allowed) == 0 {
		allowed = []string{"SG"}
	}
	providers := normalizeProviderIDs(cfg.Providers)
	actions := copyNetworkActions(cfg.Actions)
	if len(actions) == 0 {
		actions = defaultNetworkActions()
	}
	forceTimes := append([]int(nil), cfg.ForceDisconnectSecs...)

	g.mu.Lock()
	changed := g.enabled != cfg.Enabled ||
		strings.Join(g.allowedCountries, ",") != strings.Join(allowed, ",") ||
		strings.Join(g.providers, ",") != strings.Join(providers, ",") ||
		actionsKey(g.actions) != actionsKey(actions) ||
		intsKey(g.forceTimes) != intsKey(forceTimes)
	g.enabled = cfg.Enabled
	g.allowedCountries = allowed
	g.providers = providers
	g.actions = actions
	g.forceTimes = forceTimes
	if changed {
		g.nextCheck = time.Time{}
		g.providerCursor = 0
		g.actionConfirmUntil = time.Time{}
	}
	g.mu.Unlock()
}

func (g *networkGuard) skip(d time.Duration) {
	g.mu.Lock()
	until := time.Now().Add(d)
	g.skipUntil = until
	g.mu.Unlock()
	logMsg("network check skipped until %s", until.Format("15:04:05"))
	updateTooltip()
}

func (g *networkGuard) skipRemaining(now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.skipUntil.After(now) {
		return time.Until(g.skipUntil).Truncate(time.Second)
	}
	return 0
}

func (g *networkGuard) forceRemaining(now time.Time) time.Duration {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.forceUntil.After(now) {
		return time.Until(g.forceUntil).Truncate(time.Second)
	}
	return 0
}

func (g *networkGuard) cancelActiveBlock() {
	g.mu.Lock()
	wasActive := g.forceUntil.After(time.Now()) || g.forcePrompting
	g.forceUntil = time.Time{}
	g.forcePrompting = false
	g.nextForceAttempt = time.Time{}
	g.forceDisconnect = false
	g.mu.Unlock()
	if wasActive {
		logMsg("scheduled network action canceled from tray menu")
	}
	updateTooltip()
}

func (g *networkGuard) tick(now time.Time) {
	g.mu.Lock()
	if !g.enabled {
		g.mu.Unlock()
		return
	}
	if g.skipUntil.After(now) {
		g.mu.Unlock()
		updateTooltip()
		return
	}
	skipExpired := false
	if !g.skipUntil.IsZero() {
		g.skipUntil = time.Time{}
		g.nextCheck = time.Time{}
		skipExpired = true
	}
	forceTriggered, forceActive, forceUrgent, forceMsg := g.updateForceDisconnectLocked(now)
	forcePrompting := g.forcePrompting
	localScanDue := g.lastLocalScan.IsZero() || now.Sub(g.lastLocalScan) >= localIPScanInterval
	if localScanDue {
		g.lastLocalScan = now
	}
	localLogDue := g.localIPsKnown && (g.lastLocalLog.IsZero() || now.Sub(g.lastLocalLog) >= localIPLogInterval)
	if localLogDue {
		g.lastLocalLog = now
	}
	g.mu.Unlock()

	if forceMsg != "" {
		logMsg(forceMsg)
	}
	if forceTriggered {
		go g.promptScheduledDisconnect(now)
		return
	}
	if localScanDue {
		g.scanLocalIPs()
	}
	if localLogDue {
		g.logLocalIPSnapshot()
	}

	g.mu.Lock()
	if !g.enabled || g.skipUntil.After(now) {
		g.mu.Unlock()
		return
	}
	if g.forcePrompting || g.actionPrompting {
		g.mu.Unlock()
		return
	}
	if g.forceUntil.After(now) {
		forceActive = true
		forceUrgent = forceUrgent || g.forceDisconnect
		if forceUrgent || !g.nextForceAttempt.After(now) {
			g.nextForceAttempt = now.Add(disconnectRetryCooldown)
		} else {
			forceActive = false
		}
	}
	g.mu.Unlock()
	if forcePrompting {
		return
	}
	if forceActive {
		reason := "scheduled network-action window"
		if forceTriggered {
			reason = "scheduled network-action trigger"
		}
		go g.enforceScheduledDisconnect(forceUrgent, reason)
		return
	}

	g.mu.Lock()
	if g.checking || g.nextCheck.After(now) {
		g.mu.Unlock()
		if skipExpired {
			updateTooltip()
		}
		return
	}
	g.checking = true
	g.mu.Unlock()
	if skipExpired {
		updateTooltip()
	}

	go g.runCheck()
}

func (g *networkGuard) updateForceDisconnectLocked(now time.Time) (bool, bool, bool, string) {
	if len(g.forceTimes) == 0 {
		return false, g.forceUntil.After(now), false, ""
	}

	sec := now.Hour()*3600 + now.Minute()*60 + now.Second()
	slot := ""
	for _, forceSec := range g.forceTimes {
		if forceSec == sec {
			slot = fmt.Sprintf("%s/%d", now.Format("2006-01-02"), forceSec)
			break
		}
	}
	if slot == "" || slot == g.lastForceSlot {
		return false, g.forceUntil.After(now), g.forceDisconnect, ""
	}

	g.lastForceSlot = slot
	g.forcePrompting = true
	g.nextForceAttempt = time.Time{}
	g.nextCheck = time.Time{}
	return true, false, false, fmt.Sprintf("scheduled network action prompt triggered at %s", now.Format("15:04:05"))
}

func (g *networkGuard) promptScheduledDisconnect(triggeredAt time.Time) {
	disconnect := confirmScheduledDisconnect(forcePromptTimeout)
	now := time.Now()

	g.mu.Lock()
	if !g.enabled {
		g.forcePrompting = false
		g.forceUntil = time.Time{}
		g.forceDisconnect = false
		g.nextForceAttempt = time.Time{}
		g.mu.Unlock()
		logMsg("scheduled network action prompt ignored because network check is disabled")
		return
	}
	g.forcePrompting = false
	if !disconnect {
		g.forceUntil = time.Time{}
		g.forceDisconnect = false
		g.nextForceAttempt = time.Time{}
		g.mu.Unlock()
		logMsg("scheduled network action at %s dismissed; user chose manual handling", triggeredAt.Format("15:04:05"))
		return
	}

	g.forceUntil = now.Add(forceDisconnectDuration)
	g.forceDisconnect = true
	g.nextForceAttempt = now.Add(disconnectRetryCooldown)
	g.nextCheck = time.Time{}
	until := g.forceUntil
	g.mu.Unlock()

	logMsg("scheduled network action confirmed or timed out; enforcing until %s", until.Format("15:04:05"))
	g.enforceScheduledDisconnect(true, "scheduled network-action prompt")
}

func (g *networkGuard) enforceScheduledDisconnect(urgent bool, reason string) {
	g.mu.Lock()
	if !g.enabled {
		g.mu.Unlock()
		return
	}
	g.mu.Unlock()

	if err := g.runConfiguredNetworkActions(urgent); err != nil {
		logMsg("%s failed: %v", reason, err)
	}
}

func (g *networkGuard) scanLocalIPs() {
	fingerprint, err := localIPFingerprint()
	if err != nil {
		logMsg("local IP scan failed: %v", err)
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	prev := g.lastLocalIPs
	if g.localIPsKnown && fingerprint == prev {
		return
	}
	if !g.localIPsKnown {
		g.localIPsKnown = true
		g.lastLocalIPs = fingerprint
		g.lastLocalLog = time.Now()
		logMsg("local IP fingerprint initialized: %s", displayFingerprint(fingerprint))
		return
	}

	g.lastLocalIPs = fingerprint
	if fingerprint == "" {
		g.lastLocalLog = time.Now()
		logMsg("local IP fingerprint changed: %s -> %s", displayFingerprint(prev), displayFingerprint(fingerprint))
		return
	}

	g.nextCheck = time.Time{}
	g.forceDisconnect = true
	g.lastLocalLog = time.Now()
	if g.forceUntil.After(time.Now()) {
		g.nextForceAttempt = time.Time{}
		logMsg("local IP fingerprint changed: %s -> %s; enforcing scheduled network action",
			displayFingerprint(prev), displayFingerprint(fingerprint))
		return
	}
	logMsg("local IP fingerprint changed: %s -> %s; forcing public IP verification",
		displayFingerprint(prev), displayFingerprint(fingerprint))
}

func (g *networkGuard) logLocalIPSnapshot() {
	g.mu.Lock()
	fingerprint := g.lastLocalIPs
	publicInfo := g.lastInfo
	publicIP := g.lastIP
	g.mu.Unlock()

	logMsg("local IP fingerprint unchanged: %s (public_ip=%s public_info=%s)",
		displayFingerprint(fingerprint), emptyAsUnknown(publicIP), emptyAsUnknown(publicInfo))
}

func (g *networkGuard) runCheck() {
	enabled, allowed, endpoints := g.snapshot()
	if !enabled {
		g.finishCheck()
		return
	}

	ok, loc, err := checkAllowedPublicIP(allowed, endpoints)
	info := "unknown"
	if loc.Source != "" || loc.IP != "" {
		info = fmt.Sprintf("%s %s/%s %s via %s",
			loc.IP, loc.CountryCode, loc.Country, loc.City, loc.Source)
	}

	g.mu.Lock()
	prevIP := g.lastIP
	hadPrevIP := prevIP != ""
	ipChanged := loc.IP != "" && hadPrevIP && loc.IP != prevIP
	if loc.IP != "" {
		g.lastIP = loc.IP
	}
	g.lastInfo = info
	g.checking = false
	g.nextCheck = time.Now().Add(networkRecheckDelay)
	skipActive := g.skipUntil.After(time.Now())
	forceDisconnect := g.forceDisconnect
	confirmCacheCleared := false
	if ok {
		g.forceDisconnect = false
		confirmCacheCleared = !g.actionConfirmUntil.IsZero()
		g.actionConfirmUntil = time.Time{}
	}
	g.mu.Unlock()

	if err != nil {
		logMsg("network check failed: %v", err)
	} else if !hadPrevIP && loc.IP != "" {
		logMsg("network check initial IP: allowed=%v allowed_countries=%v providers=%v location=%s", ok, allowed, endpointIDs(endpoints), info)
	} else if ipChanged {
		logMsg("network check public IP changed: %s -> %s, allowed=%v allowed_countries=%v providers=%v location=%s", prevIP, loc.IP, ok, allowed, endpointIDs(endpoints), info)
	}
	if confirmCacheCleared {
		logMsg("network action confirmation cache cleared because public IP is allowed")
	}

	if !ok {
		enabled, _, _ := g.snapshot()
		if !enabled {
			logMsg("network check would run actions but checking is disabled (location=%s)", info)
			return
		}
		if skipActive {
			logMsg("network check would run actions but skip is active (location=%s)", info)
			return
		}
		if err != nil {
			logMsg("network check could not verify public IP; keeping network connected (%v)", err)
			return
		} else if ipChanged {
			logMsg("network check detected changed disallowed public IP; handling configured actions")
		} else {
			logMsg("network check detected disallowed public IP; handling configured actions (location=%s)", info)
		}
		if !g.confirmNetworkTriggeredAction(info) {
			return
		}
		if err := g.runConfiguredNetworkActions(ipChanged || forceDisconnect); err != nil {
			logMsg("network action failed: %v", err)
		}
	}
}

func (g *networkGuard) confirmNetworkTriggeredAction(info string) bool {
	g.mu.Lock()
	now := time.Now()
	if !g.enabled || g.skipUntil.After(now) {
		g.mu.Unlock()
		return false
	}
	if g.actionConfirmUntil.After(now) {
		until := g.actionConfirmUntil
		g.mu.Unlock()
		logMsg("network action confirmation cache active; skipping prompt until %s", until.Format("15:04:05"))
		return true
	}
	if g.actionPrompting {
		g.mu.Unlock()
		return false
	}
	g.actionPrompting = true
	g.mu.Unlock()

	confirmed := confirmNetworkAction(networkActionPromptTimeout, info)
	now = time.Now()

	g.mu.Lock()
	g.actionPrompting = false
	if !g.enabled || g.skipUntil.After(now) {
		g.mu.Unlock()
		return false
	}
	if !confirmed {
		until := now.Add(networkActionDismissSkip)
		g.skipUntil = until
		g.nextCheck = until
		g.forceDisconnect = false
		g.mu.Unlock()
		logMsg("network action dismissed; skipping network checks until %s", until.Format("15:04:05"))
		updateTooltip()
		return false
	}
	until := now.Add(networkActionConfirmCache)
	g.actionConfirmUntil = until
	g.mu.Unlock()
	logMsg("network action confirmed; suppressing prompts until %s", until.Format("15:04:05"))
	return true
}

func (g *networkGuard) snapshot() (bool, []string, []locationEndpoint) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.enabled, append([]string(nil), g.allowedCountries...), g.nextEndpointsLocked()
}

func (g *networkGuard) nextEndpointsLocked() []locationEndpoint {
	endpoints := endpointsByID(g.providers)
	if len(endpoints) == 0 {
		endpoints = append([]locationEndpoint(nil), locationEndpoints...)
	}
	if len(endpoints) <= 2 {
		return endpoints
	}

	ordered := make([]locationEndpoint, 0, len(endpoints))
	for i := range endpoints {
		ordered = append(ordered, endpoints[(g.providerCursor+i)%len(endpoints)])
	}
	g.providerCursor = (g.providerCursor + 2) % len(endpoints)
	return ordered
}

func (g *networkGuard) finishCheck() {
	g.mu.Lock()
	g.checking = false
	g.nextCheck = time.Now().Add(networkRecheckDelay)
	g.mu.Unlock()
}

func checkAllowedPublicIP(allowedCountries []string, endpoints []locationEndpoint) (bool, publicIPLocation, error) {
	ctx, cancel := context.WithTimeout(context.Background(), networkCheckTimeout)
	defer cancel()

	if len(endpoints) == 0 {
		return false, publicIPLocation{}, fmt.Errorf("at least 1 IP location provider is required")
	}

	var successes []publicIPLocation
	var errs []string
	for _, ep := range endpoints {
		loc, err := fetchPublicIPLocation(ctx, ep)
		if loc.Source == "" {
			loc.Source = ep.name
		}
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", loc.Source, err))
			continue
		}
		successes = append(successes, loc)
		if locationAllowed(loc, allowedCountries) {
			break
		}
	}

	if len(successes) == 0 {
		return false, publicIPLocation{}, errors.New(strings.Join(errs, "; "))
	}

	loc := combineLocations(successes)
	if anyLocationAllowed(successes, allowedCountries) {
		return true, loc, nil
	}
	return false, loc, nil
}

func combineLocations(locs []publicIPLocation) publicIPLocation {
	ipSet := make(map[string]bool)
	sourceSet := make(map[string]bool)
	countrySet := make(map[string]bool)
	citySet := make(map[string]bool)
	var ips, sources, countries, cities []string

	for _, loc := range locs {
		if loc.IP != "" && !ipSet[loc.IP] {
			ipSet[loc.IP] = true
			ips = append(ips, loc.IP)
		}
		if loc.Source != "" && !sourceSet[loc.Source] {
			sourceSet[loc.Source] = true
			sources = append(sources, loc.Source)
		}
		if loc.CountryCode != "" && !countrySet[loc.CountryCode] {
			countrySet[loc.CountryCode] = true
			countries = append(countries, loc.CountryCode)
		}
		if loc.City != "" && !citySet[loc.City] {
			citySet[loc.City] = true
			cities = append(cities, loc.City)
		}
	}
	sort.Strings(ips)
	sort.Strings(sources)
	sort.Strings(countries)
	sort.Strings(cities)

	return publicIPLocation{
		IP:          strings.Join(ips, ","),
		CountryCode: strings.Join(countries, ","),
		City:        strings.Join(cities, ","),
		Source:      strings.Join(sources, "+"),
	}
}

func anyLocationAllowed(locs []publicIPLocation, allowedCountries []string) bool {
	for _, loc := range locs {
		if locationAllowed(loc, allowedCountries) {
			return true
		}
	}
	return false
}

func locationAllowed(loc publicIPLocation, allowedCountries []string) bool {
	allowed := make(map[string]bool)
	for _, code := range allowedCountries {
		allowed[strings.ToUpper(strings.TrimSpace(code))] = true
	}
	return allowed[strings.ToUpper(strings.TrimSpace(loc.CountryCode))]
}

func normalizeProviderIDs(ids []string) []string {
	if len(ids) == 0 {
		return allProviderIDs()
	}

	selected := make(map[string]bool)
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if providerExists(id) {
			selected[id] = true
		}
	}
	if len(selected) == 0 {
		return allProviderIDs()
	}

	var out []string
	for _, ep := range locationEndpoints {
		if selected[ep.id] {
			out = append(out, ep.id)
		}
	}
	return out
}

func allProviderIDs() []string {
	out := make([]string, 0, len(locationEndpoints))
	for _, ep := range locationEndpoints {
		out = append(out, ep.id)
	}
	return out
}

func providerExists(id string) bool {
	for _, ep := range locationEndpoints {
		if ep.id == id {
			return true
		}
	}
	return false
}

func endpointsByID(ids []string) []locationEndpoint {
	selected := make(map[string]bool)
	for _, id := range ids {
		selected[id] = true
	}
	var out []locationEndpoint
	for _, ep := range locationEndpoints {
		if selected[ep.id] {
			out = append(out, ep)
		}
	}
	return out
}

func endpointIDs(endpoints []locationEndpoint) []string {
	out := make([]string, 0, len(endpoints))
	for _, ep := range endpoints {
		out = append(out, ep.id)
	}
	return out
}

func intsKey(values []int) string {
	return fmt.Sprint(values)
}

func actionsKey(actions []networkActionConfig) string {
	parts := make([]string, 0, len(actions))
	for _, action := range actions {
		parts = append(parts, action.Type+"\x00"+action.Script)
	}
	return strings.Join(parts, "\x01")
}

func networkActionLabels(actions []networkActionConfig) []string {
	labels := make([]string, 0, len(actions))
	for _, action := range actions {
		labels = append(labels, action.Type)
	}
	return labels
}

func fetchPublicIPLocation(ctx context.Context, ep locationEndpoint) (publicIPLocation, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ep.url, nil)
	if err != nil {
		return publicIPLocation{}, err
	}
	req.Header.Set("User-Agent", "SleepHook-Go/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return publicIPLocation{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return publicIPLocation{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	var raw map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return publicIPLocation{}, err
	}
	if status, _ := raw["status"].(string); strings.EqualFold(status, "fail") {
		msg, _ := raw["message"].(string)
		if msg == "" {
			msg = "lookup failed"
		}
		return publicIPLocation{}, errors.New(msg)
	}
	if success, ok := raw["success"].(bool); ok && !success {
		msg := firstString(raw, "message")
		if msg == "" {
			msg = "lookup failed"
		}
		return publicIPLocation{}, errors.New(msg)
	}

	country := firstString(raw, "country", "country_name")
	countryCode := firstString(raw, "countryCode", "country_code", "country_iso", "country_code2")
	if countryCode == "" && len(country) == 2 {
		countryCode = country
	}

	loc := publicIPLocation{
		IP:          firstString(raw, "ip", "query"),
		CountryCode: countryCode,
		Country:     country,
		City:        firstString(raw, "city"),
		Source:      ep.name,
	}
	if loc.CountryCode == "" {
		return publicIPLocation{}, fmt.Errorf("missing country code")
	}
	return loc, nil
}

func localIPFingerprint() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}

	var entries []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ifKey := fmt.Sprintf("if:%d:%s:mtu=%d:flags=%s:mac=%s",
			iface.Index, iface.Name, iface.MTU, iface.Flags.String(), iface.HardwareAddr.String())
		entries = append(entries, ifKey)

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipFromAddr(addr)
			if shouldIgnoreLocalIP(ip) {
				continue
			}
			entries = append(entries, fmt.Sprintf("addr:%d:%s=%s", iface.Index, iface.Name, ip.String()))
		}
	}

	sort.Strings(entries)
	return strings.Join(entries, "|"), nil
}

func ipFromAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}

func displayFingerprint(fingerprint string) string {
	if fingerprint == "" {
		return "<none>"
	}
	return fingerprint
}

func emptyAsUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown"
	}
	return s
}

func shouldIgnoreLocalIP(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 169 && ip4[1] == 254 {
		return true
	}
	return false
}

func firstString(raw map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := raw[key].(string); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (g *networkGuard) runConfiguredNetworkActions(urgent bool) error {
	g.mu.Lock()
	if !urgent && !g.lastDisconnect.IsZero() && time.Since(g.lastDisconnect) < disconnectRetryCooldown {
		g.mu.Unlock()
		return nil
	}
	if g.actionRunning {
		g.mu.Unlock()
		return nil
	}
	actions := copyNetworkActions(g.actions)
	if len(actions) == 0 {
		actions = defaultNetworkActions()
	}
	g.lastDisconnect = time.Now()
	g.forceDisconnect = false
	g.actionRunning = true
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		g.actionRunning = false
		g.mu.Unlock()
	}()

	return runNetworkActions(actions)
}

func copyNetworkActions(actions []networkActionConfig) []networkActionConfig {
	out := make([]networkActionConfig, len(actions))
	copy(out, actions)
	return out
}

func runNetworkActions(actions []networkActionConfig) error {
	var errs []string
	for _, action := range actions {
		switch action.Type {
		case networkActionDisconnect:
			if err := disconnectWindowsNetwork(); err != nil {
				errs = append(errs, fmt.Sprintf("disconnect: %v", err))
			}
		case networkActionPowerShell:
			if err := runPowerShellScript(action.Script); err != nil {
				errs = append(errs, fmt.Sprintf("powershell: %v", err))
			} else {
				logMsg("network action powershell executed")
			}
		default:
			errs = append(errs, fmt.Sprintf("unsupported action %q", action.Type))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func disconnectWindowsNetwork() error {
	ps := `Get-NetAdapter | Where-Object { $_.Status -eq 'Up' } | Disable-NetAdapter -Confirm:$false`
	if err := runPowerShellScript(ps); err == nil {
		logMsg("network disconnected via Disable-NetAdapter")
		return nil
	} else {
		logMsg("Disable-NetAdapter failed, trying ipconfig /release: %v", err)
	}

	if err := runHiddenCommand("ipconfig.exe", "/release"); err != nil {
		return err
	}
	logMsg("network disconnected via ipconfig /release")
	return nil
}

func runPowerShellScript(script string) error {
	return runHiddenCommand("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-ExecutionPolicy", "Bypass", "-Command", script)
}

func runHiddenCommand(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), networkActionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s timed out after %s", name, networkActionTimeout)
	}
	return err
}
