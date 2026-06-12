# FatBaby Ops Runbook — Production Traffic Hardening

*Written: 2026-06-12 | Status: ready to deploy*

---

## Architecture Overview

```
Internet
  ↓
Nginx (443/80) — proxy cache 60s, rate limit 5 req/s/IP, gzip
  ├── fatbaby.io       → newssite   :8082
  └── api.fatbaby.io   → signalapi  :9091
       ↓
Go services (systemd, auto-restart)
  ├── newssite     :8082   — HTML front-end
  ├── signalapi    :9091   — REST API (MySQL + MongoDB)
  ├── processor            — SEC filing extraction
  ├── secwatch             — EDGAR poller (every 5m)
  └── projector            — MySQL CQRS projector
       ↓
Storage
  ├── var/secwatch/    — append-only NDJSON event store (source of truth)
  ├── MySQL 8.4        — governance_signals, eps_results, entity_timeline
  └── MongoDB 7        — entities collection (flattened entity documents)
```

---

## First Deploy

### 1. Build and install

```bash
# On the server, from repo root:
bash ops/deploy.sh
```

This builds all binaries, runs tests, installs systemd units, restarts services, and reloads nginx.

### 2. Start dependencies

```bash
docker compose -f ops/docker-compose.prod.yml up -d mysql mongo
```

### 3. Copy and fill env file

```bash
cp ops/env.production /etc/fatbaby.env
# Edit /etc/fatbaby.env — fill in secrets
```

Update `EnvironmentFile` in each systemd unit if you move this path.

### 4. Install nginx configs

```bash
sudo cp ops/nginx/*.conf /etc/nginx/conf.d/
sudo nginx -t
sudo systemctl reload nginx
```

---

## Traffic Hardening Summary

| Layer | Mechanism | Config |
|-------|-----------|--------|
| nginx | Rate limit: 5 req/s/IP, burst 20 | `limit_req_zone` |
| nginx | Proxy cache: GET 60s TTL | `proxy_cache_valid 200 60s` |
| nginx | Gzip: HTML, CSS, JSON | `gzip_types` |
| nginx | Stale-while-revalidate | `proxy_cache_use_stale` |
| nginx | SSE bypass | `/live/events` no-cache, long timeout |
| Go | IP rate limit: 5 queries/day (content pages) | `ipRateLimiter` |
| Go | IP rate limit: 5 Ask Emily/day | `askRateLimiter` |
| systemd | Auto-restart on crash | `Restart=always` |
| systemd | Memory cap | `MemoryMax=512M` |

### What to do when traffic spikes

1. Check nginx cache hit rate: `grep "X-Cache-Status: HIT" /var/log/nginx/access.log | wc -l`
2. If miss rate is high: increase `proxy_cache_valid` TTL in nginx conf, reload nginx
3. If Go is the bottleneck: scale with multiple newssite instances + nginx upstream block
4. If EDGAR polling is being rate-limited: reduce `-rate-rps` on secwatch

---

## Monitoring

### Quick health check

```bash
curl -s https://fatbaby.io/healthz          # should return: ok
curl -s https://api.fatbaby.io/healthz       # should return: ok
sudo systemctl status 'fatbaby-*'
tail -f /home/fatbaby/PRRJECT_FATBABY/var/logs/*.log
```

### Service status

```bash
for svc in newssite processor secwatch signalapi projector; do
    echo -n "fatbaby-$svc: "
    sudo systemctl is-active fatbaby-$svc
done
```

### nginx cache stats

```bash
# Cache hit rate (last 1000 requests)
tail -1000 /var/log/nginx/access.log | grep -oP 'X-Cache-Status: \K\w+' | sort | uniq -c
```

---

## Incident Playbook

### newssite returning 502

1. `sudo systemctl status fatbaby-newssite` — is it running?
2. `tail -50 var/logs/newssite.log` — look for panic or OOM
3. `sudo systemctl restart fatbaby-newssite`
4. Check `MemoryMax` in systemd unit — may need to increase if event store grew

### signalapi returning 503

Means MySQL or MongoDB is not configured/reachable. The service degrades gracefully — signals
and entity endpoints return 503, but the newssite still works (no signals shown).

1. Check `docker compose -f ops/docker-compose.prod.yml ps`
2. Check `MYSQL_URL` and `MONGODB_URL` in env file
3. Run: `mysql -u fatbaby -p fatbaby -e "SELECT 1"`

### Processor stalled (no new signal_generated events)

1. `tail -50 var/logs/processor.log`
2. Common cause: SEC EDGAR rate-limited the IP. Wait 2 minutes, restart processor.
3. `sudo systemctl restart fatbaby-processor`

### nginx disk full (cache)

```bash
sudo du -sh /var/cache/nginx/fatbaby
sudo rm -rf /var/cache/nginx/fatbaby/*
sudo systemctl reload nginx
```

---

## Log Rotation

Install `/etc/logrotate.d/fatbaby`:

```
/home/fatbaby/PRRJECT_FATBABY/var/logs/*.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    sharedscripts
    postrotate
        systemctl kill -s USR1 fatbaby-newssite || true
    endscript
}
```

---

## Scaling Beyond Single Node

When a single node isn't enough:

1. **Multiple newssite instances** — add `upstream` block to nginx, load-balance round-robin
2. **MySQL read replicas** — projector writes to primary; signalapi reads from replica
3. **MongoDB Atlas** — replace self-hosted Mongo with Atlas for managed scaling
4. **S3 event archive** — implement `S3Sink` from the distributed architecture spec
   (see `docs/architecture-distributed-event-intelligence.md`)
5. **NATS JetStream** — replace file-tail polling with a real event bus

See `docs/architecture-distributed-event-intelligence.md` for the full distributed evolution plan.
