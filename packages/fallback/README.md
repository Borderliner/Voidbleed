# Fallback templates

Voidbleed installs noctalia, noctalia-greeter and adw-gtk-theme from the
Voiders Community Repository (https://repo.voiders.dev). These templates are
not built by default; they exist so Voidbleed can rehost the packages quickly
if that repository becomes unavailable:

    cp -r packages/fallback/<pkg> packages/srcpkgs/
    scripts/build-packages.sh <pkg>

Then drop 20-voiders-community.conf and the voiders key from overlays/system
and packages/keys. The noctalia-greeter INSTALL here duplicates what
voidbleed-desktop-config already does and can be removed if rehosted.
