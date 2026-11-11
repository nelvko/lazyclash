# Managed by lazyclash. Source this file; do not execute it.
status is-interactive; or return 0

function __lazyclash_snapshot
    if test "$__lazyclash_taken" = 1
        return 0
    end
    set -g __lazyclash_old_present
    set -g __lazyclash_old_exported
    for __lc_name in $__lazyclash_names
        set -l __lc_old "__lazyclash_old_$__lc_name"
        if set -qg $__lc_name
            set -ga __lazyclash_old_present 1
            set -g $__lc_old $$__lc_name
            if set -qgx $__lc_name
                set -ga __lazyclash_old_exported 1
            else
                set -ga __lazyclash_old_exported 0
            end
        else
            set -ga __lazyclash_old_present 0
            set -ga __lazyclash_old_exported 0
            set -g $__lc_old
        end
    end
    # Keep both visible original bypass lists, including universal values.
    set -g __lazyclash_bypass_original $no_proxy $NO_PROXY
    set -g __lazyclash_taken 1
    return 0
end
function __lazyclash_bypass
    set -g __lazyclash_bypass_value
    set -l __lc_items
    for __lc_value in $__lazyclash_bypass_original localhost 127.0.0.1 ::1
        for __lc_item in (string split -- , "$__lc_value")
            set __lc_item (string trim -- "$__lc_item")
            test -n "$__lc_item"; or continue
            contains -- "$__lc_item" $__lc_items; or set -a __lc_items "$__lc_item"
        end
    end
    set -g __lazyclash_bypass_value (string join , $__lc_items)
    return 0
end
function __lazyclash_restore
    test "$__lazyclash_taken" = 1; or return 0
    set -l __lc_i 1
    for __lc_name in $__lazyclash_names
        set -l __lc_old "__lazyclash_old_$__lc_name"
        if test "$__lazyclash_old_present[$__lc_i]" = 1
            if test "$__lazyclash_old_exported[$__lc_i]" = 1
                set -gx $__lc_name $$__lc_old
            else
                set -gu $__lc_name $$__lc_old
            end
        else
            # Only remove our global shadow. A universal variable is untouched.
            set -eg $__lc_name
        end
        set -eg $__lc_old
        set __lc_i (math $__lc_i + 1)
    end
    set -g __lazyclash_taken 0
    for __lc_private in __lazyclash_old_present __lazyclash_old_exported __lazyclash_bypass_original
        set -eg $__lc_private
    end
    return 0
end
function __lazyclash_sync
    test -n "$__lazyclash_dir"; or return 0
    set -l __lc_record
    read -l __lc_record 2>/dev/null < "$__lazyclash_dir/request"; or return 0
    set -l __lc_fields (string split ' ' -- "$__lc_record")
    test (count $__lc_fields) = 6; or return 0
    test "$__lc_fields[1]" = 1; or return 0
    string match -qr '^[1-9][0-9]{0,19}$' -- "$__lc_fields[2]"; or return 0
    contains -- "$__lc_fields[3]:$__lc_fields[6]" 0:0 1:0 0:1; or return 0
    test "$__lc_fields[4]" = 127.0.0.1; or return 0
    set -l __lc_rev "$__lc_fields[2]"
    set -l __lc_enabled "$__lc_fields[3]"
    set -l __lc_port "$__lc_fields[5]"
    set -l __lc_detach "$__lc_fields[6]"
    set -l __lc_er 0
    if test "$__lc_enabled" = 1
        read -l __lc_record 2>/dev/null < "$__lazyclash_endpoint"; or return 0
        set -l __lc_endpoint_fields (string split ' ' -- "$__lc_record")
        test (count $__lc_endpoint_fields) = 4; or return 0
        test "$__lc_endpoint_fields[1]" = 1; and test "$__lc_endpoint_fields[3]" = 127.0.0.1; or return 0
        string match -qr '^[1-9][0-9]{0,19}$' -- "$__lc_endpoint_fields[2]"; or return 0
        set __lc_er "$__lc_endpoint_fields[2]"
        set __lc_port "$__lc_endpoint_fields[4]"
    end
    string match -qr '^[0-9]{1,5}$' -- "$__lc_port"; or return 0
    if test "$__lc_enabled" = 1
        test "$__lc_port" -ge 1; and test "$__lc_port" -le 65535; or return 0
    end
    set -l __lc_revision "$__lc_rev:$__lc_er:$__lc_port:$__lc_detach"
    test "$__lazyclash_revision" = "$__lc_revision"; and return 0
    set -l __lc_error 0
    if test "$__lc_enabled" = 1
        if __lazyclash_snapshot
            __lazyclash_bypass
            set -gx http_proxy "http://127.0.0.1:$__lc_port"
            set -gx https_proxy "$http_proxy"
            set -gx all_proxy "socks5h://127.0.0.1:$__lc_port"
            set -gx HTTP_PROXY "$http_proxy"
            set -gx HTTPS_PROXY "$https_proxy"
            set -gx ALL_PROXY "$all_proxy"
            set -gx no_proxy "$__lazyclash_bypass_value"
            set -gx NO_PROXY "$__lazyclash_bypass_value"
            set -g __lazyclash_applied 1
            set -g __lazyclash_port "$__lc_port"
        else
            set __lc_error 1
        end
    else if __lazyclash_restore
        set -g __lazyclash_applied 0
        set -g __lazyclash_port 0
    else
        set __lc_error 1
    end
    set -l __lc_umask (umask)
    umask 077
    printf '1 %s %s %s %s %s\n' "$__lc_rev" "$__lazyclash_applied" "$__lazyclash_port" "$__lc_detach" "$__lc_error" > "$__lazyclash_dir/ack.$fish_pid.tmp"
    set -l __lc_written $status
    umask $__lc_umask
    test $__lc_written = 0; or return 0
    command mv -f -- "$__lazyclash_dir/ack.$fish_pid.tmp" "$__lazyclash_dir/ack"; or return 0
    if test "$__lc_error" = 0
        set -g __lazyclash_revision "$__lc_revision"
        if test "$__lc_detach" = 1
            for __lc_marker in LAZYCLASH_SHELL_SESSION LAZYCLASH_SHELL_PID LAZYCLASH_SHELL_LOADED
                set -eg $__lc_marker
            end
            set -g __lazyclash_dir ''
        end
    end
    return 0
end
function __lazyclash_prompt --on-event fish_prompt
    __lazyclash_sync
    return 0
end
function __lazyclash_cleanup --on-event fish_exit
    if test -n "$__lazyclash_dir"
        command rm -f -- "$__lazyclash_dir/request" "$__lazyclash_dir/ack" "$__lazyclash_dir/owner.json" "$__lazyclash_dir/endpoint"
        command rmdir -- "$__lazyclash_dir" 2>/dev/null
    end
    return 0
end
if not set -q __lazyclash_pid; or test "$__lazyclash_pid" != "$fish_pid"; or test -z "$__lazyclash_dir"
    set -g __lazyclash_pid $fish_pid
    set -l __lc_attachment (@LAZYCLASH_EXECUTABLE@ shell _attach --shell fish --pid "$fish_pid")
    test $status = 0; and test (count $__lc_attachment) = 3; or return 1
    set -g __lazyclash_session "$__lc_attachment[1]"
    set -g __lazyclash_dir "$__lc_attachment[2]"
    set -g __lazyclash_endpoint "$__lc_attachment[3]"
    test -n "$__lazyclash_session"; and test -d "$__lazyclash_dir"; and test -n "$__lazyclash_endpoint"; or return 1
    set -g __lazyclash_taken 0
    set -g __lazyclash_applied 0
    set -g __lazyclash_port 0
    set -g __lazyclash_revision ''
    set -g __lazyclash_names http_proxy https_proxy all_proxy HTTP_PROXY HTTPS_PROXY ALL_PROXY no_proxy NO_PROXY
end
set -gx LAZYCLASH_SHELL_SESSION "$__lazyclash_session"
set -gx LAZYCLASH_SHELL_PID "$fish_pid"
set -gx LAZYCLASH_SHELL_LOADED 1
__lazyclash_sync
if test -n "$__lazyclash_revision"
    @LAZYCLASH_EXECUTABLE@ shell _finalize --session "$__lazyclash_session"; or true
end
return 0
