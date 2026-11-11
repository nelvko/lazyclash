# Managed by lazyclash. Source this file; do not execute it.
[[ -o interactive ]] || return 0

__lazyclash_snapshot() {
    emulate -L zsh
    local __lc_name __lc_attr __lc_i=1
    for __lc_name in "${__lazyclash_names[@]}"; do
        __lc_attr=${parameters[$__lc_name]-scalar}
        case $__lc_attr in scalar|scalar-export) ;; *) return 1 ;; esac
    done
    [[ ${__lazyclash_taken:-0} == 1 ]] && return 0
    __lazyclash_old_values=(); __lazyclash_old_present=(); __lazyclash_old_exported=()
    for __lc_name in "${__lazyclash_names[@]}"; do
        __lazyclash_old_present[__lc_i]=${+parameters[$__lc_name]}
        if (( ${+parameters[$__lc_name]} )); then __lazyclash_old_values[__lc_i]=${(P)__lc_name}; else __lazyclash_old_values[__lc_i]=''; fi
        __lc_attr=${parameters[$__lc_name]-scalar}
        if [[ $__lc_attr == *export* ]]; then __lazyclash_old_exported[__lc_i]=1; else __lazyclash_old_exported[__lc_i]=0; fi
        (( __lc_i++ ))
    done
    __lazyclash_taken=1
    return 0
}
__lazyclash_bypass() {
    emulate -L zsh
    local __lc_item __lc_rest __lc_seen=','
    __lazyclash_bypass_value=''
    __lc_rest="${__lazyclash_old_values[7]},${__lazyclash_old_values[8]},localhost,127.0.0.1,::1,"
    while [[ -n $__lc_rest ]]; do
        __lc_item=${__lc_rest%%,*}; __lc_rest=${__lc_rest#*,}
        __lc_item=${__lc_item#${__lc_item%%[![:space:]]*}}
        __lc_item=${__lc_item%${__lc_item##*[![:space:]]}}
        [[ -n $__lc_item ]] || continue
        [[ $__lc_seen == *",$__lc_item,"* ]] && continue
        __lc_seen="$__lc_seen$__lc_item,"
        __lazyclash_bypass_value="${__lazyclash_bypass_value:+$__lazyclash_bypass_value,}$__lc_item"
    done
    return 0
}
__lazyclash_restore() {
    emulate -L zsh
    [[ ${__lazyclash_taken:-0} == 1 ]] || return 0
    local __lc_name __lc_attr __lc_i=1
    for __lc_name in "${__lazyclash_names[@]}"; do
        __lc_attr=${parameters[$__lc_name]-scalar}
        case $__lc_attr in scalar|scalar-export) ;; *) return 1 ;; esac
    done
    for __lc_name in "${__lazyclash_names[@]}"; do
        if [[ ${__lazyclash_old_present[__lc_i]} == 1 ]]; then
            typeset -g "$__lc_name=${__lazyclash_old_values[__lc_i]}" || return 1
            if [[ ${__lazyclash_old_exported[__lc_i]} == 1 ]]; then typeset -gx "$__lc_name"; else typeset -g +x "$__lc_name"; fi
        else unset "$__lc_name" || return 1; fi
        (( __lc_i++ ))
    done
    __lazyclash_taken=0
    __lazyclash_old_values=(); __lazyclash_old_present=(); __lazyclash_old_exported=()
    return 0
}
__lazyclash_sync() {
    emulate -L zsh
    local __lc_v __lc_rev __lc_enabled __lc_host __lc_port __lc_detach __lc_extra
    local __lc_ev __lc_er=0 __lc_eh __lc_ep __lc_error=0
    [[ -n ${__lazyclash_dir:-} ]] || return 0
    IFS=' ' read -r __lc_v __lc_rev __lc_enabled __lc_host __lc_port __lc_detach __lc_extra 2>/dev/null < "$__lazyclash_dir/request" || return 0
    [[ $__lc_v == 1 && -z $__lc_extra && $__lc_rev == [1-9]* && $__lc_rev == <-> && ${#__lc_rev} -le 20 ]] || return 0
    case $__lc_enabled:$__lc_detach in 0:0|1:0|0:1) ;; *) return 0 ;; esac
    [[ $__lc_host == 127.0.0.1 ]] || return 0
    if [[ $__lc_enabled == 1 ]]; then
        IFS=' ' read -r __lc_ev __lc_er __lc_eh __lc_ep __lc_extra 2>/dev/null < "$__lazyclash_endpoint" || return 0
        [[ $__lc_ev == 1 && $__lc_eh == 127.0.0.1 && -z $__lc_extra && $__lc_er == [1-9]* && $__lc_er == <-> && ${#__lc_er} -le 20 ]] || return 0
        __lc_port=$__lc_ep
    fi
    [[ $__lc_port == <-> && ${#__lc_port} -le 5 ]] || return 0
    if [[ $__lc_enabled == 1 ]]; then (( __lc_port >= 1 && __lc_port <= 65535 )) || return 0; fi
    [[ ${__lazyclash_revision:-} == "$__lc_rev:$__lc_er:$__lc_port:$__lc_detach" ]] && return 0
    if [[ $__lc_enabled == 1 ]]; then
        if __lazyclash_snapshot; then
            __lazyclash_bypass
            export http_proxy="http://127.0.0.1:$__lc_port" https_proxy="http://127.0.0.1:$__lc_port" all_proxy="socks5h://127.0.0.1:$__lc_port"
            export HTTP_PROXY="$http_proxy" HTTPS_PROXY="$https_proxy" ALL_PROXY="$all_proxy"
            export no_proxy="$__lazyclash_bypass_value" NO_PROXY="$__lazyclash_bypass_value"
            __lazyclash_applied=1; __lazyclash_port=$__lc_port
        else __lc_error=1; fi
    elif __lazyclash_restore; then __lazyclash_applied=0; __lazyclash_port=0
    else __lc_error=1; fi
    (umask 077; printf '1 %s %s %s %s %s\n' "$__lc_rev" "${__lazyclash_applied:-0}" "${__lazyclash_port:-0}" "$__lc_detach" "$__lc_error" > "$__lazyclash_dir/ack.$__lazyclash_pid.tmp" && command mv -f -- "$__lazyclash_dir/ack.$__lazyclash_pid.tmp" "$__lazyclash_dir/ack") || return 0
    if [[ $__lc_error == 0 ]]; then
        __lazyclash_revision="$__lc_rev:$__lc_er:$__lc_port:$__lc_detach"
        if [[ $__lc_detach == 1 ]]; then
            unset LAZYCLASH_SHELL_SESSION LAZYCLASH_SHELL_PID LAZYCLASH_SHELL_LOADED
            __lazyclash_dir=''
        fi
    fi
    return 0
}
__lazyclash_precmd() { __lazyclash_sync; return 0; }
__lazyclash_cleanup() {
    if [[ -n ${__lazyclash_dir:-} ]]; then
        command rm -f -- "$__lazyclash_dir/request" "$__lazyclash_dir/ack" "$__lazyclash_dir/owner.json" "$__lazyclash_dir/endpoint"
        command rmdir -- "$__lazyclash_dir" 2>/dev/null || :
    fi
    return 0
}
if [[ ${parameters[precmd_functions]-} == *readonly* || ${parameters[zshexit_functions]-} == *readonly* ]]; then return 1; fi
if [[ ${__lazyclash_pid:-} != $$ || -z ${__lazyclash_dir:-} ]]; then
    typeset -g __lazyclash_pid=$$
    typeset -g __lazyclash_attachment=$(@LAZYCLASH_EXECUTABLE@ shell _attach --shell zsh --pid "$__lazyclash_pid") || return 1
    typeset -g __lazyclash_session=${__lazyclash_attachment%%$'\n'*}
    __lazyclash_attachment=${__lazyclash_attachment#*$'\n'}
    typeset -g __lazyclash_dir=${__lazyclash_attachment%%$'\n'*}
    typeset -g __lazyclash_endpoint=${__lazyclash_attachment#*$'\n'}
    unset __lazyclash_attachment
    [[ -n $__lazyclash_session && -d $__lazyclash_dir && -n $__lazyclash_endpoint ]] || return 1
    typeset -g __lazyclash_taken=0 __lazyclash_applied=0 __lazyclash_port=0 __lazyclash_revision=''
    typeset -ga __lazyclash_names=(http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY no_proxy NO_PROXY)
    typeset -ga __lazyclash_old_values __lazyclash_old_present __lazyclash_old_exported
fi
autoload -Uz add-zsh-hook
add-zsh-hook precmd __lazyclash_precmd || return 1
add-zsh-hook zshexit __lazyclash_cleanup || return 1
export LAZYCLASH_SHELL_SESSION="$__lazyclash_session" LAZYCLASH_SHELL_PID="$__lazyclash_pid" LAZYCLASH_SHELL_LOADED=1
__lazyclash_sync
if [[ -n ${__lazyclash_revision:-} ]]; then @LAZYCLASH_EXECUTABLE@ shell _finalize --session "$__lazyclash_session" || :; fi
return 0
