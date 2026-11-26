package cli

import (
	"fmt"

	"loom/internal/llama"
	"loom/internal/store"
	"loom/internal/units"
)

func completion(args []string) error {
	if len(args) != 1 || args[0] != "fish" {
		return fmt.Errorf("usage: loom completion fish   (save it as ~/.config/fish/completions/loom.fish)")
	}
	fmt.Print(fishCompletions)
	return nil
}

func complete(st *store.Store, profiles *llama.Profiles, args []string) {
	if len(args) == 0 {
		return
	}
	switch args[0] {
	case "models":
		models, _ := st.Scan()
		for _, m := range models {
			fmt.Printf("%s\t%s, %s\n", m.Ref, units.Bytes(m.Size), kindOf(m))
		}
	case "profiles":
		if len(args) < 2 {
			return
		}
		models, _ := st.Scan()
		if m, err := resolve(models, args[1]); err == nil {
			for _, name := range profiles.Names(m.Ref) {
				fmt.Println(name)
			}
		}
	case "settings":
		for _, id := range settingIDs {
			if s, ok := llama.SettingByID(id); ok {
				fmt.Printf("%s=\t%s\n", id, s.Label)
			}
		}
	}
}

func kindOf(m store.Model) string {
	k := m.Kind().String()
	if m.MMProj != "" {
		k += "+vision"
	}
	return k
}

const fishCompletions = `complete -c loom -f

set -l cmds run pull ls rm keys completion help
set -l top "not __fish_seen_subcommand_from $cmds"
complete -c loom -n $top -a run        -d 'Start a model'
complete -c loom -n $top -a pull       -d 'Download a model from Hugging Face'
complete -c loom -n $top -a ls         -d 'List downloaded models'
complete -c loom -n $top -a rm         -d 'Delete a model'
complete -c loom -n $top -a keys       -d 'Print the key bindings'
complete -c loom -n $top -a completion -d 'Print shell completions'
complete -c loom -n $top -a help       -d 'Show help'

complete -c loom -n '__fish_seen_subcommand_from run rm; and __fish_is_nth_token 2' -a '(loom complete models)'

complete -c loom -n '__fish_seen_subcommand_from run' -l profile -d 'Start from a saved profile' -x -a '(loom complete profiles (commandline -opc)[3])'
complete -c loom -n '__fish_seen_subcommand_from run' -l chat  -d 'Chat in this terminal'
complete -c loom -n '__fish_seen_subcommand_from run' -l ctx   -d 'Context window in tokens' -x
complete -c loom -n '__fish_seen_subcommand_from run' -l port  -d 'Server port' -x
complete -c loom -n '__fish_seen_subcommand_from run' -l lan   -d 'Let your network connect'
complete -c loom -n '__fish_seen_subcommand_from run' -l tools -d 'Let the model run commands (dangerous)'
complete -c loom -n '__fish_seen_subcommand_from run' -l cpu   -d 'Do not use the GPU'
complete -c loom -n '__fish_seen_subcommand_from run' -l set   -d 'Set any launch-sheet setting' -x -a '(loom complete settings)'

complete -c loom -n '__fish_seen_subcommand_from rm' -s y -l yes -d 'Do not ask'
complete -c loom -n '__fish_seen_subcommand_from completion' -a fish
`
