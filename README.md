<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/svg/logo-on-dark.svg">
    <img src="assets/svg/logo-on-light.svg" alt="loom" width="360">
  </picture>
</p>

![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)
![llama.cpp](https://img.shields.io/badge/runs%20on-llama.cpp-8A63D2)
![Format](https://img.shields.io/badge/models-GGUF-2EA44F)
![Platform](https://img.shields.io/badge/platform-Linux-lightgrey?logo=linux&logoColor=white)
![TUI](https://img.shields.io/badge/TUI-Bubble%20Tea-FF5F87)

Find, download and run local LLMs with [llama.cpp](https://github.com/ggml-org/llama.cpp), from a terminal
interface that explains every flag it passes.

## Install

```
sudo pacman -S --needed llama-cpp ggml-vulkan
go install .
```

## Use

```
loom                   open the interface
loom run phi4          start a model, explaining each flag
loom pull owner/repo   download from Hugging Face
loom ls                list downloaded models
loom rm phi4           delete a model
loom help run          options of a command
```

## Interface

Six views, switched with `←` `→`, `tab` or `1`-`6`. `?` lists all keys.

| View     | What it does                                                                                                                         |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| Library  | Downloaded models, and downloads in progress above them (`space` pauses or resumes), with GPU fit. `enter` runs, `i` details, `D` finds it on Discover, `u` updates it when the Hub has a newer version, `/` search. |
| Discover | Search Hugging Face, see sizes, GPU fit and what you already have. `f` filters, `s` picks the sort, `S` flips its direction, `]` `[` turn the page.                                                              |
| Activity | Running servers, with the selected server's log.                                                                       |
| Doctor   | Health checks for llama.cpp, GPU, memory, disk and token. `r` rechecks.                                                              |
| Profiles | Saved launch settings per model. `r` launches, `enter` edits, `e` renames.                                                           |
| Config   | loom's own options: editor, key hints, warnings, network timeout, table columns. `d` resets.                                                         |

The launch sheet shows each choice next to the llama.cpp flag it produces. `s` saves its settings as a named profile, `p` loads one, and `loom run <model> --profile <name>` uses it. `E` edits the launch flags in your editor (changed or added lines become extra flags) (`$VISUAL`, `$EDITOR`, or set one in Config) in this terminal. `y` copies the llama.cpp command. Agent tools are off by default.

## Configuration

|                  |                                                                               |
| ---------------- | ----------------------------------------------------------------------------- |
| Models           | `~/.local/share/loom/models/` (or `$LOOM_MODELS`)                             |
| Profiles         | `~/.config/loom/profiles.json`                                                |
| Interface        | `~/.config/loom/config.json`, edited in the Config view                       |
| Key bindings     | `~/.config/loom/keys.conf`, start from `loom keys > ~/.config/loom/keys.conf` |
| Fish completions | `loom completion fish > ~/.config/fish/completions/loom.fish`                 |
| Gated models     | set `HF_TOKEN`                                                                |
