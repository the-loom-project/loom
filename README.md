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

Three views (`←` `→`, `tab`, or `1` `2` `3`). Everything else opens as a popup over the current view. `?` lists all keys.

| View     | What it does                                                                                                                                                                                              |
| -------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Library  | Downloaded models with parameters, quant, size, context, type and GPU fit. `enter` configures and runs, `r` reruns with the last settings, `i` shows details, `f` filters, `/` searches.                  |
| Discover | Searches Hugging Face with the same category and task filters as the website (`f`). Shows quants with size and GPU fit before you download. Models llama.cpp can't run are hidden, `u` shows them in red. |
| Activity | Downloads and running servers. The bottom panel follows the selected server's log.                                                                                                                        |

The launch sheet lists every choice in plain words next to the exact llama.cpp flags it produces, with a memory
estimate and warnings. `y` copies the `loom run` command and `Y` copies the plain llama.cpp command, to run outside loom.
Agent tools are off by default.

## Configuration

|                  |                                                                               |
| ---------------- | ----------------------------------------------------------------------------- |
| Models           | `~/.local/share/loom/models/` (or `$LOOM_MODELS`)                             |
| Settings         | `~/.config/loom/settings.json`                                                |
| Key bindings     | `~/.config/loom/keys.conf`, start from `loom keys > ~/.config/loom/keys.conf` |
| Fish completions | `loom completion fish > ~/.config/fish/completions/loom.fish`                 |
| Gated models     | set `HF_TOKEN`                                                                |
