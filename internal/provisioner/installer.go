package provisioner

import (
	"context"
	"fmt"

	storage "github.com/nickheyer/discopanel/internal/db"
	"github.com/nickheyer/discopanel/internal/docker"
)

// runInstallerContainer executes a java command inside a one-shot
// discopanel-runtime container with the server data dir mounted at /data.
// Installer output is forwarded to the progress sink.
func (p *Provisioner) runInstallerContainer(ctx context.Context, server *storage.Server, cfg *storage.ServerConfig, cmd []string) error {
	javaMajor := docker.RequiredJavaMajor(server.MCVersion)
	image := p.docker.RuntimeImage(javaMajor)

	uid := 1000
	gid := 1000
	if cfg.UID != nil {
		uid = *cfg.UID
	}
	if cfg.GID != nil {
		gid = *cfg.GID
	}

	opts := docker.OneShotOptions{
		Image:      image,
		Cmd:        cmd,
		DataPath:   server.DataPath,
		WorkingDir: "/data",
		User:       fmt.Sprintf("%d:%d", uid, gid),
		Name:       fmt.Sprintf("discopanel-install-%s", server.ID),
		Labels: map[string]string{
			"discopanel.server.id": server.ID,
			"discopanel.managed":   "true",
			"discopanel.oneshot":   "true",
		},
	}

	return p.docker.RunOneShot(ctx, opts, func(line string) {
		p.progress(server, "[installer] %s", line)
	})
}
