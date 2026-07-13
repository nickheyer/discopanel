# DiscoPanel Checkpoint — 2026-07-12 (branch DISCO_CONTAINERS @ staged tree = origin dcf3206)

Full-codebase review: seven subsystem passes (runtime/agent, provisioner/minecraft,
autopilot/lifecycle, proto/RPC/auth, persistence/orchestration, proxy/indexers,
frontend) plus cross-cutting scans. Every claim carries file:line and the anchor
claims were independently re-verified. Baseline health: `go build ./...` clean,
`go vet` clean, `svelte-check` 0 errors, discobench module builds, deadcode
reachability near-zero. Companion to TODO.md; this file is the checkpoint ledger.

## STATUS 2026-07-12 (this session)

DONE, gated (build/vet/test clean, svelte-check 0/0): all of B.1-B.10; all of
C.1-C.64; A.4/A.5/A.6 substance; E config.example.yaml keys, E frontend shadcn
dirs (−4,618 lines), E RBAC id-space, E proxy dead code (AllocateProxyPort,
SetRouteActive), E support/file bounded reads + race + dedupe, E alias
descriptions. Corrections to this ledger discovered while executing:
- pkg/logger New is NOT dead (five test packages use it) — stays.
- No ProxyPort model exists (E item was stale); nothing to remove.
- C.48 anchor is internal/provisioner/download.go, not pkg/download.
- B.10 as written breaks the panel (global-settings pseudo-row violates a real
  FK); shipped with a companion migration that rebuilds server_properties
  without the FK and purged 73 orphaned rows.
- C.1 bootFailureArmedAt deleted (only tests referenced it), not wired.
- Fabric exposes no structured getters (C.16); ResultAnalyzer message grammar
  is the machine surface and is what ships.
- Legacy default-role rows sourced `oidc` are dropped on next login by B.6 role
  sync (one-time migration effect, intended).
- Runtime images must be rebuilt + pushed after this lands (CI stamps version).

DONE 2026-07-13 (in-session, serial): B.4 (module role, IsModuleToken read,
plaintext dropped via migration, per-create mint+rotation); D.2-D.12 (registry
ServerLoaderForModpack + CutPackLoaderID seams, applyModpackSelection dedupe
which also fixed the broken manual-pack update path, PackPlatform descriptor
rows, pack-source-ordered depinstall with CF path, module confighash label,
container_port alias + InContainerPort + template migration, category struct
tags + test, cmd/javamajors generated matrix, sender.Run chokepoint, webhook
vars from alias context + Discord color into presets + vocab migration, alias
EvaluateCondition raw-split); D.13 magic port removed by proxy pass, interface
reshape declined as shape-only; F.6 storage half (proxy_* sample columns,
delta clamping, collector wiring) — proto exposure rides the Wave-3 gen.

DONE 2026-07-13 (E pass): proto dead surface removed with reserves (9 SLP
messages, ProxyConfig, SERVER_STATUS_RESTARTING, UsedPort.in_use, upload
temp_path incl. the HTTP handler echo, GetModpackBySlug, GetModpackFiles +
ModpackFile message, AssignRole/UnassignRole/GetUserRoles, UpdateMod +
ImportUploadedMod no-op name/description, Mod description/author/website);
new surface wired end to end (MetricsSample proxy_* incl. live ws gauge,
Server.class_count, ListModulesRequest.full_stats); CaptureArmed documented
loopback-only; ListMods enriched with jar mod_id+version via ReadModJar and
one scanModDir/modEntryID mechanism (6 uuid loops deleted); modpack proto
converter deduped 7→1; SearchModpacks favorites N+1 gone; GetIndexerStatus
uses GROUP BY count; ListModules batches lookups, never writes during read,
docker stats gated behind full_stats; module.go commented converters deleted.
Checkpoint correction: pkg/download anchor for C.48 was provisioner-local;
GetModpackByURL kept (status module calls it, paste-link UI pending F.17).

DONE 2026-07-13 (F pass): F.7 chart event strip from the ledger (server/doctor
markers over the shared range); F.9 registry install rung before disables
(nil-installer stands down the rung); F.10 incident view via clickable trace
filter in the actions channel; F.14 CF fingerprint identify (murmur2 util,
fuego POST codec, background ListMods name sweep cached per file); F.15
bring-your-world (world_upload_session_id on create, minimal NBT level.dat
reader for level-name + version testimony, staged extract); F.16 world rewind
(ListBackups/RestoreBackup RPCs over the backup dir layout, pre-restore
safety snapshot, pre-provision world snapshot hook, backups timeline on the
files tab); F.17 paste-link import through the modpacks search box;
task-execution wiring (cancel running executions, all-tasks history via
ListServerExecutions, scheduler health via GetSchedulerStatus).

DONE 2026-07-13 (closing pass): comment sweep (127 violations rewritten, census
now zero; test fixtures mimicking real crash reports deliberately untouched);
A.0 ledger hygiene applied to TODO.md (revert-and-rebuild history recorded for
the wedge killer, lag-debt math, Java 8 GC fallback, clean-exit breaker;
clientmods sha1 sweep marked not shipped; agent-off crash blindness recorded as
a deliberate trade; guardrail note downgraded to validation-only; log levels
marked done; dead file refs fixed; sections 4, 5, 8 close-outs recorded);
runtime and module images rebuilt locally via make images (pushes happen in
CI per the Makefile, trigger modules.yml to publish).

CHECKPOINT COMPLETE. TODO.md is the ledger again.

---

## A. The ledger diverged from the tree (revert debt)

The client-sweep revert (accepted churn) took load-bearing mechanisms with it, and
TODO.md still records them as shipped. The staged index is byte-identical to
origin/DISCO_CONTAINERS tip, so these are gone from the branch, not lost locally.

0. Mark as NOT shipped / reverted: clientmods sha1 sweep (7.1 v2 b), bootwatch idle
   gate (7.1 v2 c — flat timer shipped instead), lag-debt math (10.2.10), JvmSample
   GC fallback (10.1.2), guardrail clamp (§2 note), raw-exit loop breaker. Fix dead
   file refs (A.8). Mark 1a log levels DONE. Note agent-off = no crash detection as
   a deliberate trade (or fix). §5 CF fingerprint, §4 counters→history + RakNet, §8
   legacy ping + proto drift: confirmed still open.

1. **`internal/provisioner/clientmods.go` does not exist.** TODO 7.1 v2(b) claims a
   Modrinth sha1 `version_files` bulk sweep disabling `server_side=unsupported`
   projects, verdicts cached per hash. Zero references to `version_files` in the
   tree. Only the in-jar metadata sweep survives (provisioner.go:427 + CF
   gameVersions markers, curseforge.go:417). The motivating incident class — pack
   env metadata lies (oculus `server=required`) — is uncovered again.
2. **`cmd/runtime/bootwatch.go` and its idle gate do not exist.** TODO says the
   watchdog "ends the JVM once console and CPU go idle". Shipped logic
   (cmd/runtime/main.go:269-315) is a flat timer: any post-ready crash-report
   appearance → 90s → SIGTERM, no liveness check at all. See B/runtime bug 1.
3. **The lag-line TPS debt math does not exist.** TODO 10.2 item 10 ("sparse lag
   lines stay usable for 1.5x their spacing, 45s floor, 5 min cap") has no
   corresponding code; `assembleTickSample` (cmd/runtime/agent.go:313-333) derives
   TPS from busyFraction alone. See C/runtime bug 2.
4. **The Java 8 GC fallback does not exist.** TODO 10.1 item 2 says "JvmSample GC
   applies only while no gc.log window flows (the Java 8 fallback)"; JvmSample has
   no GC fields (agent.proto:221-230) and DiscoAgent.java never reads GC MX beans.
   Java 8 servers get zero GC pause data and autopilot GC findings never fire there.
   The comment at cmd/runtime/proc.go:347 repeats the false claim.
5. **`internal/lifecycle/guardrail.go` is deleted; the clamp is now validation-only.**
   `normalizeServerMemory` (services/server.go:371) rejects MemoryMax > Memory but
   `MemoryMax == Memory` passes (guaranteed OOM headroom violation), and nothing
   clamps against the real cgroup limit in the runtime (javaargs.go:42 applies
   `-Xmx` verbatim when AUTO_MEMORY is off).
6. **The raw-exit crash-loop breaker is gone.** Hub `maybeBreakCrashLoop` (TODO 1a)
   became `autopilot.CrashResponder.breakCrashLoop`, which only runs for crashes.
   Clean-exit loops (exit 0 on load failure) restart under `unless-stopped` forever
   with no breaker, no finding, no ledger trail (crashdoctor.go:377 returns on
   `!LastExitCrashed`; isCrash needs nonzero exit or a report, main.go:494-499).
7. **Agent-off servers have zero crash detection.** TODO 7.1 v2 claims "excerpt
   regex survives only for agentless servers" — that path is fully deleted. With
   `EnableAgent=false` no exit report ever reaches the panel. Defensible under
   zero-fallbacks, but undocumented and the ledger says otherwise.
8. **Dead file references in TODO.md:** `internal/lifecycle/agent.go` (agent.json
   written by lifecycle/manager.go:545-577), `internal/autopilot/autorepair.go`
   (folded into crashdoctor), `bootwatch.go`, `clientmods.go`, `maybeBreakCrashLoop`.
9. **Stale in the favorable direction:** TODO 1a's "still open: real log levels" is
   DONE — `detectLevel` ships at pkg/logger/log_streamer.go:197,240-256 (container
   lines get real levels; remaining literals are panel-side system entries).

---

## B. Security / fail-open cluster (RC blockers)

1. **Authorization fails open on unmapped procedures.** The interceptor falls
   through to `next()` for any RPC not in the maps (internal/rpc/server.go:316-333).
   Concrete instance: `UploadToMCLogs` (services/server.go:1274) is in no map
   (mapping.go), so any authenticated user — including the synthetic anonymous user
   when anonymous access is on (auth/manager.go:293) — can publish any server's
   `logs/latest.log` (player IPs, auth lines) to the public mclo.gs service.
   Fix shape: fail-closed default + a proto↔mapping bijection test exactly like
   registry_test.go does for loaders.
2. **Enforcer-nil disables all RBAC silently.** If casbin init fails the error is
   only logged and every `if s.enforcer != nil` guard (rpc/server.go:69-77, ws
   hub.go:380,492) skips authz while auth still works. Startup should be fatal.
3. **Support bundle embeds the raw sqlite DB with zero redaction**
   (services/support.go:505-514,538-576): live session JWTs, module
   `token_plaintext`, API/agent token hashes, CF API key, RCON passwords,
   management secrets — and `UploadSupportBundle` POSTs it to an external server.
4. **Module tokens are unscoped full-privilege tokens.** They carry the creating
   user's roles (often admin); `IsModuleToken` (models.go:513) is stored and never
   read; plaintext persisted forever in `modules.token_plaintext` and injected as
   env (auth/manager.go:411-425, docker/module.go:202). Compromised module
   container = the creator's whole panel.
5. **Alias reflection exposes config secrets.** `{{config.auth.jwt_secret}}` and
   OIDC client_secret resolve into module env, and `GetAvailableAliases` lists
   resolved values (alias.go:91-96,312-348). Module-edit rights = session forgery.
   Needs allow-tag or deny-list on walked fields.
6. **OIDC:** session JWT delivered via `/login?token=…` query param (oidc.go:278 —
   history/log/Referer exposure); claim-derived roles are added on login but never
   removed when the IdP drops them (oidc.go:243-245).
7. **WS auth silently downgrades** an invalid/expired token to anonymous when
   anonymous access is on (ws/hub.go:344-351).
8. **Path containment is `strings.HasPrefix` without a separator** in ~14 file
   handlers (services/file.go:83,117,192,…,717) and the zip guard
   (pkg/files/files.go:154): `/data/foo` matches `/data/foobar`. One
   `resolveServerPath` helper via `filepath.Rel` fixes boundary + duplication.
9. **Predictable default RCON password** `discopanel_<serverID[:8]>` — server IDs
   are API-visible; derivation triplicated (provisioner/files.go:88-94,
   store.go:325-328, command/sender.go:103-109). Generate random like the
   management secret four lines below it.
10. **No `_foreign_keys=on` in the DSN** (store.go:29) — see C/persistence 2; also
    the pragma block is skipped entirely if the configured path already contains `?`.

---

## C. Correctness bugs by subsystem

### Runtime / agent

1. **Wedge killer SIGTERMs healthy servers.** Post-ready crash-report appearance →
   unconditional 90s → SIGTERM (main.go:269-315). Crash-catcher mods (Not Enough
   Crashes) and Forge's removeErroring* write reports and keep running; the kill
   then *reports as a crash* because a report exists (isCrash, main.go:494).
   `watchCrashReports` is also one-shot. Fix: gate both kill paths on death
   evidence the supervisor already has (console stopped flowing, tick/CPU idle),
   and keep watching after a survivable report. `bootFailureArmedAt` (main.go:317)
   is the unreachable remnant of the deleted gate — wire it or delete it.
   **Blocks the runtime image push.**
2. **Panel TPS under load is binary (20 or ~18).** busyFraction-only math
   (agent.go:313-333) + TickSampler.java:63 counting WAITING as idle (chunk-gen
   futures park the tick thread). A real 5-TPS server charts ~18; discobench's
   external world-age TPS will contradict the panel's own telemetry.
3. **Default-on Aikar collides with user GC choice → JVM refuses to boot.**
   javaargs.go:64-75 forces G1 when no USE_*_FLAGS is set; a migrated itzg
   `JVM_OPTS` with `-XX:+UseZGC` yields "Multiple garbage collectors selected".
   The contains-check pattern already exists for ActiveProcessorCount (line 80)
   and THP (line 87) — GC selection, the likeliest override, is missing. Also
   line 77 concatenates two env vars with no separator.
4. **Every shipped runtime image reports version "dev".** main.go:29 expects
   `-X main.runtimeVersion` that neither Dockerfile.runtime:36 nor modules.yml
   passes. The runtime-digest audit story can't name what build is running.
5. **Displaced agent session leaves a zombie handler.** services/agent.go polls
   sendErr non-blocking between Receives; hub displacement (hub.go:108-126) never
   cancels the old stream's ctx — half-open leak, or two streams feeding
   HandleMessage for one server. Attach should carry a per-session cancel.
6. **GC tail can ingest the previous run's log** — first open reads from top and
   stale gc.log survives until the JVM recreates it (gclog.go:88-93). Delete it
   pre-start beside the cds.jsa cleanup (javaargs.go:114).
7. **Exit-report replay clears on Send, not delivery** (agent.go:227-232), and
   panel dedupe state is in-memory (metrics/agent.go:60-85) — panel restart +
   container restart replays an old crash as fresh.
8. Telemetry sampled and dropped: ProcSample.cpu_percent/rss_mb (proc.go:327-334),
   JvmSample.class_count — ApplyAgentProc/Jvm never read them. Consume (cleaner
   attribution than whole-container docker stats) or stop sampling.

### Autopilot / lifecycle

9. **repairTimeout bounds the whole restart including reprovision**
   (crashdoctor.go:372,454). A big CF pack reprovision exceeds 15 min → ctx dies
   mid-Ensure → follow-up setStatus uses the same dead ctx (manager.go:191) →
   server stranded in Provisioning with an open incident and nothing to re-trigger.
   Wake-on-connect already uses a detached 2h ctx (events.go:148); the restart
   needs the same. Timeout should bound plan/apply only.
10. **Vanilla/datapack-only servers never get the doctor.** respond bails to
    breakCrashLoop when GetModsPath is empty (crashdoctor.go:398-402) — but
    planRegistry is vanilla-altitude by design (world/paxi datapacks). The BMC2
    class on a vanilla server crash-loops undiagnosed. Gate the mod-shaped rungs,
    not respond.
11. **Disable-mod button stays live mid-incident** (autopilot.go:446,464-473:
    doctorActed only true after resolve/exhaust). User click mid-repair writes
    excludes outside the journal; doctor revertAll later resurrects the jar while
    the exclude survives. Gate on `j.Incident == nil` too.
12. **Stop-intent is last-writer-wins with two holes** (manager.go:351,510-514):
    (a) user Stop landing between respond's check and the doctor's Restart gets
    overwritten by the doctor's own intent — the BMC2 override survives in that
    window; (b) a *failed* user Stop (intent set, container still up) makes every
    later crash stand down (crashdoctor.go:390) while unless-stopped restarts
    forever — standDown never reaches breakCrashLoop.
13. **`unless-stopped` races the doctor** — the policy boot the doctor kills can
    consume an incident pass if it re-crashes before SIGTERM (main.go:495), so
    real attempts ≈ half of maxDoctorPasses. Consider supervisor hold-off after
    crash exit or policy "no" with panel-owned restarts.
14. **Doctor context is panel-memory only** (collector.go:72-84): after a panel
    restart, a replayed exit resurrects the finding but doctorActed=false and the
    journal's Resolved narration is skipped — raw fix button for a handled crash.
    The on-disk journal has the truth; trust it.
15. **parseReportMods is Forge-idiom-only** (crashreport.go:63, `-- Mod loading
    issue for:`) — fabric-family crashes get no report verdicts and drop to frame
    guessing. The "no loader named anywhere" comment oversells.
16. **Fabric dep failures never trigger autonomous repair**: FatalErrors.java:146-155
    probes getErrors/getIssues (Forge/NeoForge); Fabric's ModResolutionException
    exposes neither → FatalError ships with zero FailedMods on the biggest loader
    family. Extend the reflective sweep to Fabric's structured result.
17. Lifecycle holds one server struct across minutes-long provisions and full-row
    saves it repeatedly (manager.go:188,210,253) — concurrent user edits silently
    clobbered (see D/single-writer).
18. Small: crashdoctor.go:439-441 no-op continue; `_ = ctx` params
    (crashdoctor.go:269, hub SendConsole/SendChat); idle.go:169 dead param; double
    roster reset (manager.go:262,409 AND events.go:15-21 — bus path is redundant);
    crash report re-read up to 2MB on every Analyze while <24h (crashreport.go:37-51)
    — memo on path+mtime; ApplyPerformanceFix accepts any fix_id/args without
    matching a current finding (RBAC-gated, still loose).

### Persistence / orchestration

19. **Scheduler spawns overlapping executions.** next_run advances only after
    completion (scheduler.go:202-225,380) while checkAndRunDueTasks re-lists every
    CheckInterval (=docker.sync_interval, default 5s, main.go:199-201); no per-task
    in-flight dedup. A long world backup gets dozens of concurrent zips with
    save-off/save-on interleaving. Prerequisite for the world-rewind product story.
20. **FK cascades are inert** (no `_foreign_keys=on`, store.go:29) and DeleteServer
    only hand-deletes 3 tables (store.go:132-153): orphaned *enabled* tasks fire
    "server not found" forever; ServerActions/TaskExecutions/FindingDismissals
    accumulate; never-started module rows + their API tokens leak
    (services/server.go:1067-1081 skips empty ContainerID). The "database cascade
    will delete module records" comment is false.
21. **Status monitor lost-update race** (cmd/discopanel/main.go:336-352): holds a
    listed snapshot, then full-row `store.UpdateServer` (GORM Save) on status
    change — can revert freshly-written ContainerID/RuntimeDigest/DockerImage;
    every flap also rewrites the properties row (store.go:124-130). Targeted
    column update is the fix.
22. **Panel shutdown bypasses lifecycle** (main.go:388-404): raw StopContainer, no
    ledger entry, no stop-intent, DB left "running" (doctor may read a crash next
    boot); sequential 25s stops inside one 30s ctx starves HTTP shutdown.
23. **Module containers read "starting" for 15 min**: ContainerHealth applies
    HealthStartupGrace to any container without SLP records (collector.go:509-542);
    modules never get SLP → waitForRunning dependency gates (60s) break and the UI
    shows perpetual "starting". Modules should bypass the Minecraft health checker.
24. **RCON host binding collides**: server.Port+10 on 127.0.0.1 is never reserved
    by allocation (docker/client.go:308-310) — a server on 25575 collides with a
    25565 server's RCON and fails to start.
25. **Module health ticker panic**: `time.NewTicker(0)` panics before the
    interval==0 fallback (module/manager.go:352-355); replaced ticker leaks.
26. **EXEC hook passes the whole command as one argv element** (hooks.go:161) — any
    command with arguments fails; runInitCommand (manager.go:266) already wraps
    correctly.
27. **backupDB overwrites the single .pre-migrate.bak on every boot**
    (migrations.go:40-44,301-320) — one bad migration + one restart destroys the
    good backup. Skip when nothing is pending.
28. **datetime() normalization only covers metrics** (store.go:981,996,1173):
    session expiry and next_run compare lexically with raw local-offset binds —
    wrong across DST/TZ on bare-metal hosts. store.go:165 already documents the
    driver's inconsistency; apply the one pattern everywhere.
29. Once-task Disabled set in memory, never persisted (scheduler.go:441-444).
30. Shared `*ServerMetrics` handed out lock-free (collector.go:279-293) while
    updateMetrics writes under mu — PlayerSample slice tears; "gets a copy"
    comment is false.
31. InitBuiltinTemplates runs twice per boot and reverts user edits to builtin
    templates every start (main.go:210, module/manager.go:63).
32. FindWorldDir hunts a dir literally named "world" (pkg/files/files.go:20-76)
    though the panel writes `level-name`; renamed worlds break backups, WorldSize,
    and BlueMap's hardcoded `/world` mount (builtin_templates.go:89, which then
    fails container create — read-only binds get no CreateMountpoint).

### Proxy / indexers

33. **UDP session table unbounded** (udp.go:206): spoofed-source flood on an
    exposed Geyser port = fd exhaustion (socket+goroutine per source, 5 min idle).
    Needs max-sessions or creation rate bound.
34. **CF pagination latent bug**: `pageIndex := offset/limit` passed as CF `index`,
    which is an item offset (fuego/adapter.go:60); first pager breaks (overlap) —
    plus divide-by-zero on limit 0. Currently only offset-0 callers.
35. **403 = "author blocked" conflation** (fuego.go:327-330): invalid/expired CF
    key (also 403, classified ErrAuth then discarded) silently converts every pack
    file to the CDN-guess path instead of surfacing auth failure.
36. **Singleflight followers can't cancel** (httpclient.go:69): follower blocks
    until the winner finishes (winner can take minutes under 429 cooldown ladders).
    DoChan + select on ctx.
37. **ETag cache bounds are entry-count only** (resilience.go:37-39,127-140):
    256 × 2MB = 512MB worst case per credential; random single eviction; `states`
    map never evicts rotated credentials.
38. **UpdateProxyConfig disable is a no-op** (services/proxy.go:183-194): saves
    config, never stops the manager, reports running=true until restart.
39. **UpdateRoute doesn't flush live UDP sessions** (udp.go:139-145): clients relay
    to a dead backend IP up to 5 min after module recreation. Manager.Stop clears
    proxies but not listenerPorts (manager.go:164) → module-vs-listener
    misclassification after stop/start.
40. **Pre-1.6 legacy pings pin a goroutine+conn for the full 10s** handshake
    timeout before dropping (minecraft.go:290-297); TODO §8's synth-response item
    also removes the hold.
41. RemoveRoute deletes stats entries; any future history feed must clamp negative
    deltas (minecraft.go:162-179 + reconcile removals) — counters reset per
    stop/start.
42. http.go:77-90: new ReverseProxy per request; no X-Forwarded-For on either path
    (module web UIs see proxy IP — same gap PROXY v2 fixed for Minecraft).

### Provisioner / minecraft

43. **Java major falls back to NEWEST on any Mojang API failure** and is re-derived
    live per container decision (docker/images.go:21-35,77-82) though the launch
    spec and server.JavaVersion already hold the resolved truth
    (lifecycle/manager.go:201). Piston-meta blip at start → java25 image for a
    Java-8 pack → confighash happily recreates wrong. Also user-visible at create:
    services/server.go:569, modpack.go:442,594. Subject (persisted spec) should
    drive; network fills first-create only. Version manifest cache also refuses to
    serve stale after TTL on refetch failure (versions.go:71-108) → GetLatestVersion
    "0", empty version lists.
44. **SLP readString caps status JSON at 32,767 bytes** (slp.go:343) while the
    packet frame allows 1MB — Forge FML mod lists exceed 32KB on 200+ mod packs, so
    exactly the target workload reads unreachable. Cap at the packet bound. Also
    slp.go:102-105 pong-mismatch branch is a duplicated no-op with a lying comment.
45. **installModrinthProjects can block every start** (files.go:47 → aborts Ensure,
    provisioner.go:163): slug typo or Modrinth outage prevents a fully-installed
    server from starting; re-queries versions per project on every boot
    (modrinth.go:355).
46. **Wholesale server packs never testify their MC version**
    (curseforge.go:493-535): launches with user-entered MCVersion though the tree
    holds evidence (forge libraries path already globbed by detectForgeLaunch,
    version.json in server.jar). Wrong guess → wrong Java → doctor cleanup.
47. **Release-channel filters fall through silently**: CF picks newest-by-date
    ignoring ReleaseType (curseforge.go:154-167, alpha installable);
    resolveModrinthVersion (modrinth.go:131) and installModrinthProjects
    (modrinth.go:374) fall back to versions[0] when nothing matches the allowed
    channel — installing what the setting excluded, silently.
48. **Distro/meta endpoints bypass the resilience layer entirely**
    (download.go:169-216): fresh client per call, no retry/limiter/etag for
    fabric/quilt/paper/purpur/forge/neoforge/piston meta — hit far more often per
    provision than the indexers that got hardened.
49. **mcmod.info unread** (modscan.go:163-197): 1.12-era CF packs are
    metadata-blind — dep solver, duplicate detection, client-only sweep, jar votes
    all see zero mods; nothing declares the 1.13+ boundary.
50. **Unscoped dep solve when no dialect testifies** (ResolveDialects nil →
    SolveDeps checks every dialect's manifests, depsolver.go:60,89): violates
    "uncertainty never reports" at platform level; preflightFix acts on those
    duplicates (provisioner.go:195).
51. CAS never re-verifies on get (cas.go:40-55) — bit-rot propagates to every
    future server; pruneCaches leaves empty shard dirs (cas.go:105).
52. OverrideWhitelist can't empty a whitelist (files.go:271-276 early-return).
53. management-server-secret regenerates on every Ensure incl. verified no-ops
    (files.go:99-113) — file desyncs from live process until next boot.
54. EULA gate fires after the multi-GB install (files.go:180) — check before
    install(), it's free.

### Frontend

55. **Live metrics never flow unless the console tab was opened**: only
    ServerConsole calls wsClient.connect() (server-console.svelte:227);
    subscribeMetrics returns early unauthenticated (websocket.svelte.ts:355).
    Landing on the overview (default tab) → charts load history once, never append.
56. **Login-path sessions never start the status poller**: bootstrap() runs once at
    root-layout mount and early-returns on /login (+layout.svelte:132-144); login
    navigates client-side (goto) so the layout never remounts — no 10s polling, no
    layout-seeded fetch until hard reload.
57. **LOADER_LABELS is a 30-row parallel loader taxonomy, already wrong**
    (server-status.ts:139-175): AUTO_CURSEFORGE↔CURSEFORGE labels inverted vs
    registry.go:274,282. Backend displayName is already fetchable via
    stores/loaders.ts — which 3 of 4 call sites bypass (server-settings.svelte:192,
    servers/new/+page.svelte:109, modpacks/+page.svelte:112 shadowing the store's
    export name).
58. **Trigger catalog stops at PLAYER_LEAVE** (lib/utils/events.ts:10): DEATH=7,
    ADVANCEMENT=8, CHAT=9 exist in proto and on the bus; TODO §7.4/7.5's "webhook
    tasks can already subscribe today" is API-only. Three rows close it.
59. Reconnect resets console tail to 500 (websocket.svelte.ts:431 hardcodes,
    :322 discards caller's tail) — user's line-cap selection silently reverts.
60. disconnect() has zero callers (logout leaves the old identity's socket open)
    and doesn't clear metricsSubscriptions refcounts (websocket.svelte.ts:137-146).
61. server-performance.svelte:196 polls GetServerPerformanceReport every 10s
    unconditionally (stopped servers, dialog closed).
62. Start/stop/restart/recreate switch + toast + refetch hand-rolled 4×
    (+page.svelte:160, servers/+page.svelte:148, servers/[id]/+page.svelte:150,
    command-palette.svelte:54) — one server-actions helper.
63. servers/new/+page.svelte:237 maps `config['mod_loader']` via TS enum-name
    reflection with silent UNSPECIFIED fallback over a value that is sometimes an
    indexer name, sometimes a loader name (see D.3).
64. Server.serverVersion/protocolVersion (SLP-derived) never rendered — users only
    see configured mcVersion, not what's running.

---

## D. Coupling inventory (registry escapes and parallel mechanisms)

1. **Frontend LOADER_LABELS** — C.57. Delete the table; render backend displayName.
2. **Indexer→loader mapping as three divergent brand-string switches**:
   services/server.go:396-401, server.go:893-923 (also duplicates ~60 lines of
   modpack-config assembly from CreateServer), modpack.go:291-302 — and
   SyncModpacks raw-casts an indexer string into storage.ModLoader
   (modpack.go:439) while ImportUploadedModpack correctly uses
   minecraft.MatchModLoader (modpack.go:586). One normalization seam through the
   registry.
3. **Two mechanisms for CF manifest loader ids**: provisioner's exact codec
   (curseforge.go:392 strings.Cut) vs modpack.go:586 fuzzy MatchModLoader
   (threshold 0.5, registry.go:390) over the same "forge-47.2.0" strings. Keep the
   codec, delete the fuzzy path for this input class.
4. **Pack-platform knowledge as three coordinated switches** on {Modrinth, CF,
   AutoCF}: desiredModpackFor (provisioner.go:264), ForceIncludePatterns
   (modscan.go:487), packExcludeField (modscan.go:554). A pack-platform descriptor
   on registry rows collapses them; FTB-proper would currently mean finding all
   three plus loaderInstallers.
5. **depinstall hardwired to Modrinth** (depinstall.go:29 constructs
   modrinth.NewClient directly): CF-pack missing deps that aren't on Modrinth
   always fall to disabling the dependent (crashdoctor.go:659). The server's pack
   source should drive which indexers are tried.
6. **Module recreate detection is a hand-maintained field list**
   (services/module.go:734-858) — the weaker parallel of DesiredConfigHash; give
   modules the same hash mechanism (confighash itself relies on hand-bumping "v2",
   confighash.go:28 — acceptable, but document the bump rule beside the field list).
7. **Builtin templates hardcode backend port 25565** (builtin_templates.go:39,62;
   docker/module.go:196 DISCOPANEL_SERVER_PORT): direct-mode servers listen on
   server.Port in-container (client.go:280-283) — Geyser/exporter break on custom
   ports. Root fix: expose "container port" as an alias.
8. **properties key→category 80-line switch** (services/properties.go:416-498) —
   parallel taxonomy; belongs as a struct tag beside each field (the reflection
   test from the ship-blocker pass can enforce it).
9. **Java version lists hand-synced in three places**: images.go:15-18, Makefile,
   modules.yml:93,108 ("sync with" comments). Generate the workflow matrix from
   images.go (small `go run` emitting the list).
10. **SendCommand duplicated end-to-end** (ws/hub.go:475-546 vs
    services/server.go:1214-1271): gates, echo, ledger — sync by luck. One
    chokepoint.
11. **Webhook templateData is a third templating dialect** with Discord
    presentation baked into generic delivery (webhook.go:173-227, the "TODO: Use
    the alias package!!!"). Derive the flat map from the alias context; move
    Discord embeds into a preset — do this before the 7.4 Discord module.
12. **Module hook comparators** (hooks.go:230 self-flagged): operator-splitting
    after substitution breaks when values contain `>`/`==`; belongs beside alias,
    which owns substitution.
13. **Proxier interface is a leaky union** (tcp.go:40-66, udp.go:119-145 ignore
    hostname keys; 3 of 5 methods shape-only) + containerPort==0→8081 magic
    (manager.go:518-520).

---

## E. Dead code / placeholders / drift

Backend:
- proxy: AllocateProxyPort (manager.go:452-474, zero callers, obsolete ProxyPort
  model); SetRouteActive (minecraft.go:195-204) + vestigial Active flag
  (UpsertServerRoute forces true → lookupRoute's !Active branch unreachable).
- runtime: bootFailureArmedAt (main.go:317, unreachable — see C.1 decision);
  pkg/logger New (logger.go:32).
- proto surface generating dead code in two languages: all 9 SLP* messages
  (minecraft.proto:76-129, slp.go doesn't use them), ProxyConfig
  (common.proto:191), SERVER_STATUS_RESTARTING (no storage equivalent, frontend
  styles it anyway), UsedPort.in_use (always true), upload temp_path (leaks
  server paths). RPCs with zero frontend callers: GetModpackBySlug,
  GetModpackFiles (superseded by GetModpackVersions), GetUserRoles/assignRole/
  unassignRole (user-settings edits roles via updateUser — adopt or drop),
  GetModpackByURL (wire it: paste-a-link import, see G).
- services/module.go:79-110 commented-out converters; modpack.go IndexedModpack→
  proto literal copy-pasted 6× (~150 lines); mod.go dir-scan/uuid loop 4×;
  server.go:643-651 ignores io.Copy error (truncated CFModpackZip).
- Mod proto fields (description/version/mod_id/author/website) never populated;
  UpdateMod display_name/description accepted and persisted nowhere (silent no-op
  API, mod.go:405-427); ModpackFile still lacks mod_loader/server_pack_file_id/
  version_number the DB row carries; GetModpackFiles fabricates SortIndex.
  modscan already parses real jar metadata — wire into ListMods (see G).
- CaptureArmed rides the panel envelope but the hub drops it silently — forward
  or document loopback-only (agent.proto:100-104 vs 210-211).

Frontend:
- 21 vendored shadcn dirs with no importers (toggle-group, navigation-menu,
  pagination, slider, calendar, drawer, data-table, menubar, input-otp,
  radio-group, chart, scroll-area, resizable, form, range-calendar, context-menu,
  carousel, breadcrumb, collapsible, hover-card, aspect-ratio).
- Task-execution surface half-wired: cancelExecution, listServerExecutions,
  getSchedulerStatus have zero callers (no cancel button, no scheduler health).

Hygiene:
- ~296 comments violate the caveman rule (one-time sweep).
- config.example.yaml missing: auto_migrate, docker.dns, OIDC extra_claims_*,
  reject_unmapped, required_claim, required_values.
- alias descriptions render "The 's <path>" (alias.go:212,271-309, empty prefix).
- efficiency: ListModules 4-5 round trips per module + DB writes during read
  (module.go:483-515, needs full_stats treatment); SearchModpacks N+1 favorites
  (modpack.go:119); GetIndexerStatus loads 10k rows to count (modpack.go:870);
  GetFile + GetApplicationLogs read whole files unbounded (file.go:136,
  support.go:786 — polled); support.go bundles map raced from goroutines
  (support.go:40,165,184,197) and ~100-line assembly block duplicated (73-179 vs
  207-308).
- RBAC id-space drift: tasks scope by server_id in List but task id in
  Get/Update/Delete/Toggle/Trigger; modules likewise (mapping.go:119-127,145-156;
  ResourceScopeSource documents the inconsistency) — "tasks on server X" role is
  inexpressible.
- GetNextAvailablePort ignores additional_ports and module host ports
  (server.go:1396-1410).

---

## F. Low-hanging fruit, ranked by leverage/cost

1. RBAC fail-closed + bijection test + map UploadToMCLogs (XS-S, closes B.1/B.2).
2. `_foreign_keys=on` + targeted delete sweep (S, closes B.10/C.20).
3. javaargs GC contains-check + separator fix (XS, un-breaks itzg migrants, C.3).
4. Wedge liveness gate: console-flow + CPU idle before SIGTERM, keep watching
   after survivable reports (S, unblocks runtime image push, C.1).
5. Frontend: 3 event-type rows (XS, C.58); connect WS in subscribeMetrics (XS,
   C.55); bootstrap on auth-change not mount (S, C.56); delete LOADER_LABELS (XS,
   C.57).
6. Proxy counters → metrics_samples: GetRouteStats callback into the existing 30s
   history loop + columns + reset clamping (S, closes TODO §4 thread).
7. Chart event annotations: bus events + ledger already exist; thin
   GetServerActions/events overlay on the charts (S, TODO §3 follow-up).
8. Wrong-platform jar DepIssue from jarDialect/activeMetas (~10 lines, S).
9. Registry install rung: actionInstall{ModID: namespace} before disable rungs —
   id-gated InstallModByID already exists (S-M, completes the BMC2 remedy).
10. Incident history view: filter ledger by incident-<ms> trace (S).
11. SLP cap → packet bound (XS, C.44). Runtime version ldflags (XS, C.4). Stale
    gc.log delete (XS, C.6). Legacy ping synth (S, C.40).
12. Java major from persisted spec, network only at first create + stale-serve
    manifest cache (S-M, C.43).
13. Distro endpoints onto the resilience HTTPClient (M, C.48).
14. CF fingerprint API: murmur2 + one POST codec on existing GetFilesByIDs shape
    (M, unlocks Mod metadata for loose jars, TODO §5 close-out).
15. Bring-your-world import (7.10): one small NBT level.dat reader pays twice
    (server-pack version testimony C.46 + import); upload/extract/detection
    primitives all exist (M).
16. World rewind (7.9): ListBackups/RestoreBackup RPCs over existing destDir
    layout + ExtractArchive + pre-provision snapshot hook — fix C.19 first (M).
17. getModpackByURL paste-link import UI (S, RPC exists).

## G. Suggested refactor workstreams (the checkpoint's "major refactor")

1. **Fail-closed trust boundary**: B.1-B.10 as one pass — fail-closed interceptor
   + bijection test, bundle redaction, module-token scoping (read IsModuleToken,
   mint narrow role), alias field allow-tags, OIDC cookie/fragment delivery +
   role sync, resolveServerPath helper everywhere, random RCON secret (one
   derivation site).
2. **Single-writer store discipline**: column-scoped update methods
   (UpdateServerStatus, UpdateServerContainer, UpdateServerAgentSpec…); forbid
   whole-row Save outside create; every container mutation through lifecycle
   (UpdateServerRouting C/D, shutdown path, one SendCommand chokepoint);
   scheduler in-flight dedup + next_run-before-run; datetime() everywhere.
3. **Registry completion**: D.1-D.9 — displayName from API, one indexer→loader
   seam, one CF-manifest codec, pack-platform descriptor rows, depinstall driven
   by pack source, category tags, module confighash, container-port alias,
   generated CI matrix.
4. **Runtime truthfulness**: C.1-C.8 — evidence-gated kills, debt math + WAITING
   fix (un-binary TPS), GC-choice respect, version stamping, deterministic gc
   window, spec-driven Java major, session displacement cancel; then rebuild and
   PUSH the runtime image (panel pulls tags).
5. **Ledger hygiene**: apply A.0 to TODO.md so the next session stops chasing
   ghosts.
