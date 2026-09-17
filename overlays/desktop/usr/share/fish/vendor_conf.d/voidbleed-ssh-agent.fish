# Voidbleed: use gnome-keyring's SSH agent (started by the niri config)
if test -z "$SSH_AUTH_SOCK"; and test -S "$XDG_RUNTIME_DIR/keyring/ssh"
    set -gx SSH_AUTH_SOCK "$XDG_RUNTIME_DIR/keyring/ssh"
end
