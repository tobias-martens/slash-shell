# slash: shell integration for bash (>= 4). Load it with:
#   command -v slash >/dev/null && eval "$(slash init bash)"
#
# Layer 1: on Enter, a line that is a slash phrase is replaced by the real
#          command plus a "#slash: …" comment; bash runs and records it as usual.
# Layer 2: one function per verb, for everything layer 1 doesn't rewrite
#          (compound lines, no line editor, …).
# Both fail open: if slash is missing, fails or is slow, nothing changes.
# No `return` at top level: this text is eval'd inside the user's .bashrc.

if [[ $- == *i* ]] && (( BASH_VERSINFO[0] >= 4 )); then

__slash_verbs=" @VERBS@ "

# Is the first word of "$1" a slash verb that slash may handle in this shell?
__slash_candidate() {
    local trimmed=${1#"${1%%[![:space:]]*}"}
    local first=${trimmed%%[[:space:]]*}
    [[ $__slash_verbs == *" $first "* ]] || return 1
    # The user's own aliases and functions with that name always win.
    alias -- "$first" >/dev/null 2>&1 && return 1
    if declare -F -- "$first" >/dev/null && [[ $(declare -f -- "$first") != *__slash_fn* ]]; then
        return 1
    fi
    return 0
}

__slash_resolve() {
    command -v slash >/dev/null 2>&1 || return 127   # fail open, silently
    if command -v timeout >/dev/null 2>&1; then
        timeout 3 slash resolve "$@"
    else
        slash resolve "$@"
    fi
}

# Layer 1, part 1 (bound via bind -x): rewrite the line, then choose whether
# the second key of the Enter macro accepts it or only redraws it.
__slash_enter() {
    local keymap=${1:-emacs}
    bind -m "$keymap" '"\C-x9b": accept-line'
    __slash_candidate "$READLINE_LINE" || return 0
    local out rc
    out=$(__slash_resolve --line "$READLINE_LINE" 2>/dev/null)
    rc=$?
    case $rc in
        0) READLINE_LINE=$out; READLINE_POINT=${#out} ;;
        3) READLINE_LINE=$out; READLINE_POINT=${#out}
           bind -m "$keymap" '"\C-x9b": redraw-current-line' ;;  # let the user confirm
    esac
    return 0
}

# Install layer 1 only where Enter does the default thing; if another tool
# owns Enter, leave it alone and rely on layer 2.
__slash_bind() {
    local keymap=$1 key
    for key in '\C-m' '\C-j'; do
        bind -m "$keymap" -p 2>/dev/null | grep -qxF "\"$key\": accept-line" || continue
        bind -m "$keymap" -x "\"\\C-x9a\": __slash_enter $keymap"
        bind -m "$keymap" '"\C-x9b": accept-line'
        bind -m "$keymap" "\"$key\": \"\\C-x9a\\C-x9b\""
    done
}
for __slash_km in emacs vi-insert vi-command; do
    __slash_bind "$__slash_km" 2>/dev/null
done
unset __slash_km

# Layer 2: show, ask if needed, record, run.
# bash can't add a history entry without replacing the current one, so the
# current entry is rewritten into its real equivalent instead (slash only
# does that if the phrase occurs in it exactly once).
__slash_run() {
    local entry="" out rc
    if [[ -o history ]]; then
        entry=$(HISTTIMEFORMAT= builtin history 1)
        [[ $entry =~ ^\ *[0-9]+\*?\ \ (.*)$ ]] && entry=${BASH_REMATCH[1]} || entry=""
    fi
    if [[ -n $entry ]]; then
        out=$(__slash_resolve --words --history "$entry" "$@")
    else
        out=$(__slash_resolve --words "$@")
    fi
    rc=$?
    local run=${out%%$'\n'*} newentry=""
    [[ $out == *$'\n'* ]] && newentry=${out#*$'\n'}
    case $rc in
        0)
            printf '→ %s\n' "${run%  #slash:*}" >&2 ;;
        3)
            printf '→ %s\n' "$run" >&2
            local answer
            read -r -p "Run it? [y/N] " answer
            [[ $answer == [yY]* ]] || return 1 ;;
        2)
            return 2 ;;
        *)
            command "$@"   # not ours, or slash failed: run the real program
            return ;;
    esac
    [[ -n $newentry ]] && history -s -- "$newentry"
    eval -- "${run%  #slash:*}"
}

for __slash_v in $__slash_verbs; do
    if ! alias -- "$__slash_v" >/dev/null 2>&1 && ! declare -F -- "$__slash_v" >/dev/null; then
        eval "$__slash_v() { : __slash_fn; __slash_run $__slash_v \"\$@\"; }"
    fi
done
unset __slash_v

fi
