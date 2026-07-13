# discobench

Reproducible benchmarks of Minecraft server container runtimes:
`discopanel-runtime` versus `itzg/minecraft-server`, on identical hardware,
memory, heap, JVM flag policy, world seed, and `server.properties`. The
runtime is the only variable.

## Running

```sh
cd test/discobench
go run . -config discobench.yaml -out results
```

Useful flags: `-iterations`, `-bots`, `-duration`, `-scenario <name>`,
`-contender <name>`, `-keep` (keep server data dirs for inspection).
Output is `results/results.json` (every raw number) and `results/report.md`
(median [min-max] comparison tables).

Requirements: Docker socket access, outbound network for jar downloads and
image pulls, and roughly `memory_mb` of free RAM per concurrently running
server (one server runs at a time).

## What is measured

Per scenario x contender cell, over N iterations:

- **Image size** and pull time (first pull only).
- **Cold start**: container start to first server list ping response, and
  to the vanilla `Done` console line. For itzg this includes its entrypoint
  work (jar download, config rendering); for discopanel-runtime it includes
  nothing extra because provisioning happened before start. That asymmetry
  is the product difference being measured, not an unfairness: both begin
  from "the user pressed start for the first time".
- **Warm restart**: stop, start again on the same data dir, time to `Done`.
  Both contenders skip worldgen here; entrypoint overhead and class cache
  effects dominate.
- **Idle RSS** after a 20 second settle, from docker stats with
  reclaimable file cache excluded.
- **Load phase**: N protocol-level bots join (offline mode) and fly radial
  paths outward to `walk_radius` and back, each on its own heading, forcing
  real chunk generation, chunk sends, and entity tracking. During the phase
  the harness records container CPU and RSS from docker stats, and TPS from
  the outside (below). The first `ramp_skip` of samples is discarded.
- **Graceful stop** time and exit code.
- **Stability**: bot reconnects, join failures, failed iterations.

## External TPS measurement

The observer bot records the `worldAge` field of every clientbound Set Time
packet (sent every 20 ticks) against the wall clock. World age advances
exactly one per tick regardless of gamerules, so its slope is the server's
true tick rate, measured with zero server-side footprint. This is the same
technique client-side TPS HUD mods use. No contender needs spark, Carpet,
or any mod installed, which keeps the comparison fair for all runtimes.

Reported as median / p5 / min over per-interval rates: median is the
steady experience, p5 and min are the stutters players actually feel.
Averages alone hide those and are not reported without them.

## Fairness rules

- Identical container memory limit and identical `MEMORY` heap on both
  sides; Aikar flags enabled on both (itzg `USE_AIKAR_FLAGS`, discopanel
  default). discopanel-runtime's extra defaults (CDS archive, THP,
  compact object headers where applicable) stay on, because shipped
  defaults are what is being compared; A/B any flag via per-contender
  `env` in the config.
- Identical `server.properties`: pinned seed, view and simulation
  distance, `online-mode=false` (bots), `allow-flight=true` (bots hold
  altitude without physics simulation; same setting both sides).
- One server at a time; nothing else should run on the host during a
  bench. Cold and warm starts are reported separately, never conflated.
- Worldgen cost is present in both cold boots and both load phases, and
  the pinned seed makes it the same work on both sides.

## Variance

Minecraft server benchmarks are notoriously noisy (see Meterstick,
arXiv 2112.06963, which measured peak latency degrading to 20x the mean
across identical runs). discobench mitigates rather than pretends:
multiple iterations with median and range reported per metric, ramp
discarding, fixed seed, staggered bot joins, and raw per-iteration data
preserved in `results.json`. Treat single-iteration runs as smoke tests,
not results. Defaults (3 iterations, 30 bots, 5 minutes) fit a coffee
break; publishable numbers want 5+ iterations and 10+ minutes.

## Limitations

- Bot logins speak protocol 767, so bot-load scenarios are pinned to
  MC 1.21.1 (`bots_supported: true`). Other versions still measure
  startup, stop, and memory (set `bots_supported: false`).
- Bots fly fixed radial paths: worst-case-realistic for chunk load, but
  no combat, redstone, or block interaction.
- Modpack scenarios (CurseForge/Modrinth) are not yet wired; the
  contender abstraction has room for them once the pack provisioning
  step is scripted here.
