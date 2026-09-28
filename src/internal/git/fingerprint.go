package git

import (
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// StateFingerprint lit HEAD, packed-refs et les arborescences de refs
// analysées directement sur disque (aucun processus git lancé) et en
// combine une empreinte : n'importe quel commit, fusion, changement de
// branche, création/suppression de branche ou de tag modifie l'un de ces
// fichiers et fait donc varier la valeur renvoyée. HEAD est propre au
// worktree, les refs sont partagées (commonDir) ; le répertoire reftable
// couvre les dépôts qui utilisent ce format de stockage des refs (git
// 2.45+) à la place des fichiers de refs et de packed-refs.
//
// En mode remote, seules les branches du remote sont lues : un commit local
// ne change rien à l'analyse et ne doit pas déclencher de rechargement.
func (r *Repository) StateFingerprint() string {
	h := fnv.New64a()

	if head, err := os.ReadFile(filepath.Join(r.gitDir, "HEAD")); err == nil {
		h.Write(head)
	}

	if info, err := os.Stat(filepath.Join(r.commonDir, "packed-refs")); err == nil {
		fmt.Fprintf(h, "packed-refs:%d:%d", info.Size(), info.ModTime().UnixNano())
	}

	if r.remote != "" {
		writeTreeFingerprint(h, filepath.Join(r.commonDir, "refs", "remotes", r.remote))
	} else {
		writeTreeFingerprint(h, filepath.Join(r.commonDir, "refs", "heads"))
	}
	writeTreeFingerprint(h, filepath.Join(r.commonDir, "refs", "tags"))
	writeTreeFingerprint(h, filepath.Join(r.commonDir, "reftable"))

	return fmt.Sprintf("%x", h.Sum64())
}

// writeTreeFingerprint ajoute à w le chemin relatif, la taille et la date de
// modification de chaque fichier sous root, dans un ordre stable. Un
// répertoire absent n'ajoute rien.
func writeTreeFingerprint(w io.Writer, root string) {
	var entries []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			rel = path
		}
		entries = append(entries, fmt.Sprintf("%s:%d:%d", rel, info.Size(), info.ModTime().UnixNano()))
		return nil
	})
	sort.Strings(entries)
	for _, e := range entries {
		io.WriteString(w, e)
	}
}
