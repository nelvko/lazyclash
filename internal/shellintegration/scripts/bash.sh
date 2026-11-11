# Managed by lazyclash. Source this file; do not execute it.
case $- in *i*) ;; *) return 0 ;; esac

__lazyclash_snapshot() {
    local __lc_name __lc_decl __lc_flags __lc_i=0
    # Reject attributes which make a scalar environment update unsafe.
    for __lc_name in "${__lazyclash_names[@]}"; do
        __lc_decl=$(declare -p "$__lc_name" 2>/dev/null) || __lc_decl='declare --'
        __lc_flags=${__lc_decl#declare }; __lc_flags=${__lc_flags%% *}
        case $__lc_flags in --|-x) ;; *) return 1 ;; esac
    done
    if [ "${__lazyclash_taken:-0}" = 1 ]; then return 0; fi
    __lazyclash_old_values=(); __lazyclash_old_present=(); __lazyclash_old_exported=()
    for __lc_name in "${__lazyclash_names[@]}"; do
        __lazyclash_old_present[__lc_i]=${!__lc_name+x}
        __lazyclash_old_values[__lc_i]=${!__lc_name-}
        __lc_decl=$(declare -p "$__lc_name" 2>/dev/null) || __lc_decl='declare --'
        case $__lc_decl in 'declare -x '*) __lazyclash_old_exported[__lc_i]=1 ;; *) __lazyclash_old_exported[__lc_i]=0 ;; esac
        __lc_i=$((__lc_i+1))
    done
    __lazyclash_taken=1
}

__lazyclash_bypass() {
    local __lc_item __lc_rest __lc_seen=','
    __lazyclash_bypass_value=''
    __lc_rest="${__lazyclash_old_values[6]},${__lazyclash_old_values[7]},localhost,127.0.0.1,::1,"
    while [ -n "$__lc_rest" ]; do
        __lc_item=${__lc_rest%%,*}; __lc_rest=${__lc_rest#*,}
        __lc_item=${__lc_item#"${__lc_item%%[![:space:]]*}"}
        __lc_item=${__lc_item%"${__lc_item##*[![:space:]]}"}
        [ -n "$__lc_item" ] || continue
        case $__lc_seen in *",$__lc_item,"*) continue ;; esac
        __lc_seen="$__lc_seen$__lc_item,"
        __lazyclash_bypass_value="${__lazyclash_bypass_value:+$__lazyclash_bypass_value,}$__lc_item"
    done
}

__lazyclash_restore() {
    [ "${__lazyclash_taken:-0}" = 1 ] || return 0
    local __lc_name __lc_decl __lc_flags __lc_i=0
    for __lc_name in "${__lazyclash_names[@]}"; do
        __lc_decl=$(declare -p "$__lc_name" 2>/dev/null) || __lc_decl='declare --'
        __lc_flags=${__lc_decl#declare }; __lc_flags=${__lc_flags%% *}
        case $__lc_flags in --|-x) ;; *) return 1 ;; esac
    done
    for __lc_name in "${__lazyclash_names[@]}"; do
        if [ "${__lazyclash_old_present[__lc_i]}" = x ]; then
            printf -v "$__lc_name" '%s' "${__lazyclash_old_values[__lc_i]}" || return 1
            if [ "${__lazyclash_old_exported[__lc_i]}" = 1 ]; then export "$__lc_name"; else export -n "$__lc_name"; fi
        else
            unset "$__lc_name" || return 1
        fi
        __lc_i=$((__lc_i+1))
    done
    __lazyclash_taken=0
    __lazyclash_old_values=(); __lazyclash_old_present=(); __lazyclash_old_exported=()
}

__lazyclash_sync() {
    local __lc_v __lc_rev __lc_enabled __lc_host __lc_port __lc_detach __lc_extra
    local __lc_ev __lc_er=0 __lc_eh __lc_ep __lc_error=0
    [ -n "${__lazyclash_dir:-}" ] || return 0
    IFS=' ' read -r __lc_v __lc_rev __lc_enabled __lc_host __lc_port __lc_detach __lc_extra 2>/dev/null < "$__lazyclash_dir/request" || return 0
    [ "$__lc_v" = 1 ] && [ -z "$__lc_extra" ] || return 0
    case $__lc_rev in ''|0*|*[!0-9]*) return 0 ;; esac
    [ ${#__lc_rev} -le 20 ] || return 0
    case $__lc_enabled:$__lc_detach in 0:0|1:0|0:1) ;; *) return 0 ;; esac
    [ "$__lc_host" = 127.0.0.1 ] || return 0
    if [ "$__lc_enabled" = 1 ]; then
        IFS=' ' read -r __lc_ev __lc_er __lc_eh __lc_ep __lc_extra 2>/dev/null < "$__lazyclash_endpoint" || return 0
        [ "$__lc_ev" = 1 ] && [ "$__lc_eh" = 127.0.0.1 ] && [ -z "$__lc_extra" ] || return 0
        case $__lc_er in ''|0*|*[!0-9]*) return 0 ;; esac
        [ ${#__lc_er} -le 20 ] || return 0
        __lc_port=$__lc_ep
    fi
    case $__lc_port in ''|*[!0-9]*) return 0 ;; esac
    [ ${#__lc_port} -le 5 ] || return 0
    if [ "$__lc_enabled" = 1 ]; then [ "$__lc_port" -ge 1 ] && [ "$__lc_port" -le 65535 ] || return 0; fi
    if [ "${__lazyclash_revision:-}" = "$__lc_rev:$__lc_er:$__lc_port:$__lc_detach" ]; then return 0; fi
    if [ "$__lc_enabled" = 1 ]; then
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
    if [ "$__lc_error" = 0 ]; then
        __lazyclash_revision="$__lc_rev:$__lc_er:$__lc_port:$__lc_detach"
        if [ "$__lc_detach" = 1 ]; then
            unset LAZYCLASH_SHELL_SESSION LAZYCLASH_SHELL_PID LAZYCLASH_SHELL_LOADED
            __lazyclash_dir=''
        fi
    fi
}

__lazyclash_prompt() {
    local __lc_status=$1
    __lazyclash_sync
    return "$__lc_status"
}
__lazyclash_cleanup() {
    if [ -n "${__lazyclash_dir:-}" ]; then
        command rm -f -- "$__lazyclash_dir/request" "$__lazyclash_dir/ack" "$__lazyclash_dir/owner.json" "$__lazyclash_dir/endpoint"
        command rmdir -- "$__lazyclash_dir" 2>/dev/null || :
    fi
}

# A readonly prompt configuration cannot host an additive hook.
__lazyclash_pc_decl=$(declare -p PROMPT_COMMAND 2>/dev/null) || __lazyclash_pc_decl='declare --'
__lazyclash_pc_flags=${__lazyclash_pc_decl#declare }; __lazyclash_pc_flags=${__lazyclash_pc_flags%% *}
case $__lazyclash_pc_flags in *r*) unset __lazyclash_pc_decl __lazyclash_pc_flags; return 1 ;; esac
unset __lazyclash_pc_decl __lazyclash_pc_flags

# These variables are deliberately not exported: child shells take their own snapshot.
if [ "${__lazyclash_pid:-}" != "${BASHPID:-$$}" ] || [ -z "${__lazyclash_dir:-}" ]; then
    __lazyclash_pid=${BASHPID:-$$}
    __lazyclash_attachment=$(@LAZYCLASH_EXECUTABLE@ shell _attach --shell bash --pid "$__lazyclash_pid") || return 1
    __lazyclash_session=${__lazyclash_attachment%%$'\n'*}
    __lazyclash_attachment=${__lazyclash_attachment#*$'\n'}
    __lazyclash_dir=${__lazyclash_attachment%%$'\n'*}
    __lazyclash_endpoint=${__lazyclash_attachment#*$'\n'}
    unset __lazyclash_attachment
    [ -n "$__lazyclash_session" ] && [ -d "$__lazyclash_dir" ] && [ -n "$__lazyclash_endpoint" ] || return 1
    __lazyclash_taken=0; __lazyclash_applied=0; __lazyclash_port=0; __lazyclash_revision=''
    __lazyclash_names=(http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY no_proxy NO_PROXY)
fi
export LAZYCLASH_SHELL_SESSION="$__lazyclash_session" LAZYCLASH_SHELL_PID="$__lazyclash_pid" LAZYCLASH_SHELL_LOADED=1
case $(declare -p PROMPT_COMMAND 2>/dev/null) in
    'declare -a '*)
        __lazyclash_found=0
        for __lazyclash_entry in "${PROMPT_COMMAND[@]}"; do case $__lazyclash_entry in *'__lazyclash_prompt '*) __lazyclash_found=1 ;; esac; done
        if [ "$__lazyclash_found" = 0 ]; then PROMPT_COMMAND+=( '__lazyclash_prompt "$?" && :' ); fi
        unset __lazyclash_found __lazyclash_entry ;;
    *) case ${PROMPT_COMMAND:-} in *'__lazyclash_prompt '*) ;; *) PROMPT_COMMAND="${PROMPT_COMMAND:+$PROMPT_COMMAND
}__lazyclash_prompt \"\$?\" && :" ;; esac ;;
esac
# Bash has no additive EXIT hook; never replace an existing user trap.
if [ -z "$(trap -p EXIT)" ]; then trap '__lazyclash_cleanup' EXIT; fi
__lazyclash_sync

if [ -n "${__lazyclash_revision:-}" ]; then @LAZYCLASH_EXECUTABLE@ shell _finalize --session "$__lazyclash_session" || :; fi
