# slash: shell integration for zsh (>= 5.3). Load it with:
#   command -v slash >/dev/null && eval "$(slash init zsh)"
#
# Layer 1: on Enter, a line that is a slash phrase is replaced by the real
#          command plus a "#slash: …" comment; zsh runs and records it as usual.
# Layer 2: one function per verb, for everything layer 1 doesn't rewrite
#          (compound lines, no line editor, …).
# Both fail open: if slash is missing, fails or is slow, nothing changes.
#
# zsh only treats "#" as a comment when interactive_comments is set. slash
# doesn't change that option for the user: it is switched on for a line that
# carries a "#slash:" comment only, and switched back before the line runs.

if [[ -o interactive ]]; then

typeset -g __slash_verbs=" @VERBS@ "
typeset -gi __slash_ic=0

# Is the first word of "$1" a slash verb that slash may handle in this shell?
__slash_candidate() {
    emulate -L zsh
    local -a words=(${=1})
    local first=$words[1]
    [[ -n $first && $__slash_verbs == *" $first "* ]] || return 1
    # The user's own aliases and functions with that name always win.
    (( $+aliases[$first] )) && return 1
    (( $+functions[$first] )) && [[ $functions[$first] != *__slash_fn* ]] && return 1
    return 0
}

__slash_resolve() {
    emulate -L zsh
    (( $+commands[slash] )) || return 127   # fail open, silently
    if (( $+commands[timeout] )); then
        timeout 3 slash resolve "$@"
    else
        slash resolve "$@"
    fi
}

# Not under emulate -L: the option must outlive this function until the line
# has been parsed. __slash_comments_off switches it back.
__slash_comments_on() {
    if [[ $1 == *'  #slash: '* && ! -o interactive_comments ]]; then
        setopt interactive_comments
        __slash_ic=1
    fi
}
__slash_comments_off() {
    if (( __slash_ic )); then
        unsetopt interactive_comments
        __slash_ic=0
    fi
}
autoload -Uz add-zsh-hook
add-zsh-hook preexec __slash_comments_off   # runs after the line is parsed
add-zsh-hook precmd __slash_comments_off    # in case the line didn't run

# Layer 1: rewrite the line, then accept it (or leave it for the user to
# confirm with a second Enter).
__slash_accept_line() {
    if __slash_candidate "$BUFFER"; then
        local out rc
        out=$(__slash_resolve --line "$BUFFER" 2>/dev/null)
        rc=$?
        case $rc in
            0) BUFFER=$out ;;
            3) BUFFER=$out; CURSOR=$#BUFFER; return 0 ;;  # let the user confirm
        esac
    fi
    __slash_comments_on "$BUFFER"
    zle accept-line
}
zle -N __slash_accept_line

# Install layer 1 only where Enter does the default thing; if another tool
# owns Enter, leave it alone and rely on layer 2.
() {
    local km key
    for km in emacs viins vicmd; do
        for key in '^M' '^J'; do
            [[ $(bindkey -M $km $key 2>/dev/null) == "\"$key\" accept-line" ]] &&
                bindkey -M $km $key __slash_accept_line
        done
    done
}

# Layer 2: show, ask if needed, record, run. The typed line stays in history;
# its real equivalent is added after it.
__slash_run() {
    emulate -L zsh
    local entry=${history[$HISTCMD]} out rc
    # A line hidden by hist_ignore_space lingers in $history: don't copy it.
    [[ -o hist_ignore_space && $entry == ' '* ]] && entry=""
    if [[ -n $entry ]]; then
        out=$(__slash_resolve --words --history "$entry" "$@")
    else
        out=$(__slash_resolve --words "$@")
    fi
    rc=$?
    local run=${out%%$'\n'*} newentry=""
    [[ $out == *$'\n'* ]] && newentry=${out#*$'\n'}
    case $rc in
        0)  # slash's own verbs (search, …) show what they run themselves
            [[ $run == "slash "* ]] || print -r -- "→ ${run%  #slash:*}" >&2 ;;
        3)
            print -r -- "→ $run" >&2
            local answer
            read -r "answer?Run it? [y/N] "
            [[ $answer == [yY]* ]] || return 1 ;;
        2)
            return 2 ;;
        *)
            command "$@"   # not ours, or slash failed: run the real program
            return ;;
    esac
    [[ -n $newentry ]] && print -s -r -- "$newentry"
    eval -- "${run%  #slash:*}"
}

() {
    local v
    for v in ${=__slash_verbs}; do
        (( $+aliases[$v] || $+functions[$v] )) && continue
        functions[$v]=": __slash_fn; __slash_run $v \"\$@\""
    done
}

fi
