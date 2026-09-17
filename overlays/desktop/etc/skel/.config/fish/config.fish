# Voidbleed — fish defaults

# User-installed tools (pipx, uv, cargo install --root ~/.local, scripts).
fish_add_path --path "$HOME/.local/bin"

if status is-interactive
    set -g fish_greeting "Bleed into the void."
    type -q starship; and starship init fish | source
end
