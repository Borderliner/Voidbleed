# fish completion for vpm (void package management utility for XBPS)
# Void ships only a bash completion for vpm; this mirrors it for fish.

set -l all_pkg_cmds info in filelist fl listfiles deps dep dependencies reverse rv search s install i yesinstall devinstall
set -l installed_pkg_cmds listalternative listalternatives la setalternative setalternatives sa reconfigure rc forceinstall fi remove rm removerecursive rr
set -l file_cmds searchfile sf remotesearchfile rsf whatprovides wp
set -l subcmds sync sy update upgrade up listrepos lr repolist rl addrepo ar $all_pkg_cmds $installed_pkg_cmds $file_cmds list ls cleanup clean cl autoremove help h helppager help-pager hp

complete -c vpm -f

# global options
complete -c vpm -l color -x -a 'yes no auto' -d 'Colorized output'
complete -c vpm -l help -d 'Show usage information'
complete -c vpm -l help-pager -d 'Show usage information in a pager'
complete -c vpm -l show-translations -d 'Show XBPS command translations'
complete -c vpm -l verbose -d 'Show XBPS command translations during execution'

# subcommands
set -l n "not __fish_seen_subcommand_from $subcmds"
complete -c vpm -n $n -a sync -d 'Synchronize remote repository data'
complete -c vpm -n $n -a update -d 'Update the system'
complete -c vpm -n $n -a listrepos -d 'List configured repositories'
complete -c vpm -n $n -a addrepo -d 'Add an additional repository'
complete -c vpm -n $n -a info -d 'Show information about package'
complete -c vpm -n $n -a filelist -d 'Show file list of package'
complete -c vpm -n $n -a deps -d 'Show dependencies of package'
complete -c vpm -n $n -a reverse -d 'Show reverse dependencies of package'
complete -c vpm -n $n -a search -d 'Search for package by name'
complete -c vpm -n $n -a searchfile -d 'Search installed package containing file'
complete -c vpm -n $n -a remotesearchfile -d 'Search remote package containing file'
complete -c vpm -n $n -a whatprovides -d 'Search for package containing file'
complete -c vpm -n $n -a list -d 'List installed packages'
complete -c vpm -n $n -a install -d 'Install packages'
complete -c vpm -n $n -a devinstall -d 'Install packages and their -devel packages'
complete -c vpm -n $n -a forceinstall -d 'Force installation of packages'
complete -c vpm -n $n -a listalternatives -d 'List alternative candidates'
complete -c vpm -n $n -a setalternative -d 'Set alternative for package'
complete -c vpm -n $n -a reconfigure -d 'Re-configure installed package'
complete -c vpm -n $n -a remove -d 'Remove packages'
complete -c vpm -n $n -a removerecursive -d 'Remove packages and their dependencies'
complete -c vpm -n $n -a cleanup -d 'Clean up cache directory'
complete -c vpm -n $n -a autoremove -d 'Remove orphaned packages'
complete -c vpm -n $n -a help -d 'Show usage information'
complete -c vpm -n $n -a helppager -d 'Show usage information in a pager'

# arguments
complete -c vpm -n "__fish_seen_subcommand_from $all_pkg_cmds" -a '(__fish_print_xbps_packages)'
complete -c vpm -n "__fish_seen_subcommand_from $installed_pkg_cmds" -a '(__fish_print_xbps_packages -i)'
complete -c vpm -n "__fish_seen_subcommand_from $file_cmds" -F
