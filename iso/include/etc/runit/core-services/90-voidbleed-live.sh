# Voidbleed live ISO: pick the session from the kernel command line.
# (live image only; the installer must not copy this file)
#
#   default                      greetd logs the live user into niri
#   nomodeset | voidbleed.console  no graphical session, autologin on tty1

if grep -qE '(^| )(nomodeset|voidbleed\.console)( |$)' /proc/cmdline; then
    msg "Voidbleed live: console mode, graphical session disabled"
    [ -r /etc/default/live.conf ] && . /etc/default/live.conf
    touch /etc/sv/greetd/down
    sed -i "s|GETTY_ARGS=\"--noclear\"|GETTY_ARGS=\"--noclear -a ${USERNAME:-anon}\"|" \
        /etc/sv/agetty-tty1/conf
fi

# The live session logs in without a password, so pam_gnome_keyring cannot
# create a login keyring and the first app storing a secret pops up a "choose
# password for new keyring" dialog. Give the live user an unencrypted default
# keyring instead (plaintext secrets: acceptable only on the live ISO).
[ -r /etc/default/live.conf ] && . /etc/default/live.conf
live_user="${USERNAME:-anon}"
keyrings="/home/$live_user/.local/share/keyrings"
if [ -d "/home/$live_user" ] && [ ! -e "$keyrings/default" ]; then
    mkdir -p "$keyrings"
    printf 'login' >"$keyrings/default"
    printf '[keyring]\ndisplay-name=Login\nctime=0\nmtime=0\nlock-on-idle=false\nlock-after=false\n' \
        >"$keyrings/login.keyring"
    chmod 700 "$keyrings"
    chmod 600 "$keyrings/default" "$keyrings/login.keyring"
    chown -R "$live_user:$live_user" "/home/$live_user/.local"
fi
