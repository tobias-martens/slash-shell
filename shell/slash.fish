# slash: shell integration for fish (>= 3.1). Loaded by conf.d/slash.fish:
#   command -q slash; and slash init fish | source
#
# Layer 1: on Enter, a line that is a slash phrase is replaced by the real
#          command plus a "#slash: …" comment; fish runs and records it as usual.
# Layer 2: one function per verb, for everything layer 1 doesn't rewrite
#          (compound lines, other tools owning Enter, …).
# Both fail open: if slash is missing, fails or is slow, nothing changes.

if status is-interactive

set -g __slash_verbs @VERBS@

# Is the first word of $argv[1] a slash verb that slash may handle here?
function __slash_candidate
    set -l first (string match -r -- '^\s*(\S+)' $argv[1])[2]
    contains -- "$first" $__slash_verbs; or return 1
    # The user's own functions (fish aliases are functions) and abbreviations win.
    abbr -q -- $first; and return 1
    if functions -q -- $first
        set -l details (functions --details --verbose -- $first)
        test "$details[5]" = __slash_fn; or return 1
    end
    return 0
end

function __slash_resolve
    command -q slash; or return 127   # fail open, silently
    if command -q timeout
        timeout 3 slash resolve $argv
    else
        slash resolve $argv
    end
end

# Layer 1: rewrite the line, then run it (or leave it for the user to confirm
# with a second Enter).
function __slash_enter
    set -l line (commandline | string collect)
    if __slash_candidate "$line"
        set -l out (__slash_resolve --line "$line" 2>/dev/null)
        switch $status
            case 0
                commandline -r -- "$out"
            case 3
                commandline -r -- "$out"
                commandline -f repaint
                return
        end
    end
    commandline -f execute
end

# Install layer 1 only where Enter does the default thing (no user binding);
# if the user or another tool owns Enter, leave it alone and rely on layer 2.
# Runs again whenever the key binding mode changes (fish_vi_key_bindings, …).
function __slash_bind --on-variable fish_key_bindings
    for mode in default insert
        for key in \r \n
            bind --user -M $mode $key >/dev/null 2>&1
            and not bind --user -M $mode $key 2>/dev/null | string match -q '*__slash_enter'
            and continue
            bind -M $mode $key __slash_enter
        end
    end
end
__slash_bind

# Layer 2: show, ask if needed, record, run. fish adds the typed line to
# history after it ran, so slash remembers it here.
function __slash_preexec --on-event fish_preexec
    set -g __slash_cmdline $argv[1]
end

function __slash_can_record
    # history append and { … } blocks need fish 4.1
    set -l v (string split . -- (string match -r -- '^\d+\.\d+' $version))
    test "$v[1]" -gt 4; or test "$v[1]" -eq 4 -a "$v[2]" -ge 1; or return 1
    set -q fish_private_mode; and return 1
    string match -q -- ' *' $__slash_cmdline; and return 1
    if functions -q fish_should_add_to_history
        fish_should_add_to_history $__slash_cmdline; or return 1
    end
    return 0
end

function __slash_run
    set -l out
    if set -q __slash_cmdline[1]; and __slash_can_record
        set out (__slash_resolve --words --history "$__slash_cmdline" $argv)
    else
        set out (__slash_resolve --words $argv)
    end
    set -l rc $status
    set -l run (string replace -r -- '  #slash:.*' '' $out[1])
    switch $rc
        case 0
            printf '→ %s\n' $run >&2
        case 3
            printf '→ %s\n' $out[1] >&2
            read -l -P "Run it? [y/N] " answer
            string match -qi -- 'y*' "$answer"; or return 1
        case 2
            return 2
        case '*'
            command $argv   # not ours, or slash failed: run the real program
            return
    end
    set -q out[2]; and history append -- $out[2]
    eval $run
end

for __slash_v in $__slash_verbs
    if not functions -q -- $__slash_v
        eval "function $__slash_v --description __slash_fn; __slash_run $__slash_v \$argv; end"
    end
end
set -e __slash_v

end
