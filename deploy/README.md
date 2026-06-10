# Deploying locus-go to locus2 (a8-apps)

locus2 currently runs the **JVM** build of Locus, deployed by **herman** from
`server-app-configs/a8-apps/dev/locus2/application.hocon` (artifact `a8-locus_3`,
mainClass `a8.locus.LocusMain`). Its supervisor program runs the nix java
launcher `/home/dev/apps/locus2/bin/locus2`.

This switches locus2 to the Go binary.

## One-time: supervisor config (JVM launcher → Go binary)

The committed source-of-truth is
`proxmox-hosts/nixgen/a8-apps/supervisor/managed/locus2.conf` (consumed by
`proxmox-hosts/bin/single-flake-deploy.py`, which pushes `/config/supervisor/` to
the host). It has been changed to:

```ini
command = /home/dev/bin/locus -config config/config.hocon
directory = /home/dev/apps/locus2
environment = TZ="America/New_York"
```

`deploy/locus2.supervisor.conf` here is a standalone copy for reference.

Apply it with the normal proxmox-hosts deploy (needs root@a8-apps access):

```bash
cd ../proxmox-hosts && ./deploy a8-apps      # single-flake-deploy.py
```

⚠️ **herman caveat:** that `.conf` is *generated* by herman from
`application.hocon`. Re-running a herman deploy for locus2 will regenerate it back
to the JVM launcher. This change is therefore transitional — durable integration
means teaching herman/`application.hocon` to describe a binary (non-JVM) app, or
removing locus2 from herman's management. The config file
(`/home/dev/apps/locus2/config/config.hocon`) is still placed there by the last
herman deploy and is reused as-is.

## Each deploy: build + ship the binary + restart

From the locus-go repo (needs `go` + ssh to a8-apps):

```bash
nix develop -c ./deploy.sh
```

This builds `linux/amd64`, scp's to `/home/dev/bin/locus`, restarts `locus2`, and
probes `http://localhost:7001/`.

## Order for the cutover

1. `./deploy.sh --no-restart`  — get the Go binary onto the host first.
2. Apply the supervisor config change (proxmox-hosts deploy above).
3. `supervisorctl restart locus2` (or re-run `./deploy.sh`).
4. Verify: `curl -s https://locus2.accur8.net/repos | head` and spot-check an
   artifact / `maven-metadata.xml` against `locus.accur8.net` (still on the JVM)
   for parity.

## Rollback

Revert `locus2.conf`'s `command` to `/home/dev/apps/locus2/bin/locus2`, re-deploy
the supervisor config, and `supervisorctl restart locus2`.
