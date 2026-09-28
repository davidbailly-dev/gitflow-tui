# gitflow-tui

![Screenshot001](assets/screenshot-001.png)

Un TUI (interface en mode texte) pour visualiser la structure GitFlow d'un dépôt Git : quelles branches existent, ce qui a été fusionné où, et les écarts par rapport au workflow attendu.

**Lecture seule** : l'outil n'effectue aucune action d'écriture sur le dépôt.

## Prérequis

- Go 1.22 ou supérieur
- Git

## Installation

Depuis le dossier `src/` du projet :

```sh
go build -o gitflow-tui ./cmd/gitflow-tui
```

Place ensuite le binaire où tu veux (par exemple dans un dossier de ton `PATH`) pour pouvoir le lancer depuis n'importe quel dépôt.

## Utilisation

Lance `gitflow-tui` depuis n'importe quel répertoire à l'intérieur d'un dépôt Git :

```sh
cd /chemin/vers/mon-depot
gitflow-tui
```

`gitflow-tui --version` (ou `-v`) affiche la version et quitte.

La branche principale peut s'appeler `main` ou `master` (si les deux existent, `main` est retenue).

L'affichage se met à jour de lui-même dès que le dépôt change (commit, fusion, changement, création ou suppression de branche), y compris depuis un autre terminal ou un worktree ; `r` force un rechargement.

## Les deux vues

### Vue Colonnes

Trois panneaux, façon navigateur de fichiers :

1. **Branches** — toutes les branches, groupées par type GitFlow (`main`, `develop`, `feature/*`, `release/*`, `hotfix/*`, `autre`).
2. **Commits / fusions** — pour `main` et `develop`, leur historique direct ; pour les autres branches, leurs seuls commits propres, y compris une fois la branche fusionnée. Les commits de fusion sont repérés (`⑂`, en orange) ; les autres commits sont en vert.
3. **Contenu** — le détail complet du commit sélectionné (message, fichiers, diff). Si c'est une fusion, la liste des commits qu'elle a apportés est affichée en premier.

### Vue Graphe

Une frise chronologique horizontale : `main` et `develop` sur leurs propres lignes, les branches éphémères (actives ou déjà fusionnées et supprimées) en dessous, chacune reliée par un trait vertical à son repère `┬` sur la ligne principale. Les branches sont placées de gauche à droite par date de première fusion, ou de création si elles ne sont pas encore fusionnées.

Elle a deux modes :
- **Historique** — ce qui s'est passé, y compris les branches supprimées reconstituées depuis les messages de fusion (git, GitHub, GitLab, Bitbucket), et les écarts détectés par rapport au workflow GitFlow : fusion vers une cible inattendue, release ou hotfix jamais refusionnée dans l'une de ses cibles, commit direct hors fusion, branche partie du mauvais parent, synchronisation directe `develop` → `main`. Par défaut, seuls ces **écarts** sont affichés ; `a` bascule vers l'historique complet.
- **Direct** — uniquement les branches encore présentes localement et pas encore complètement fusionnées, sans analyse de l'historique.

Les intégrations de pull request en squash (sujet terminé par `(#12)`) ne sont pas signalées comme commits directs. Une intégration en « rebase and merge » ne laisse en revanche aucune trace reconnaissable : ses commits apparaissent comme directs.

## Raccourcis clavier

| Touche | Action |
|---|---|
| `Tab` | Basculer entre la vue Colonnes et la vue Graphe |
| `m` | Basculer Historique / Direct (vue Graphe) |
| `a` | Basculer écarts seulement / historique complet (vue Graphe, mode Historique) |
| `↑` `↓` / `j` `k` | Se déplacer dans le panneau actif (ou faire défiler le contenu) |
| `←` `→` / `h` `l` | Changer de panneau (vue Colonnes) |
| `/` | Filtrer les branches par nom (vue Colonnes) |
| `r` | Rafraîchir les données |
| `?` | Afficher/masquer l'aide |
| `q` / `Ctrl+C` | Quitter |
