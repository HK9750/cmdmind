# CmdMind Bash integration template.
# Run `cmdmind init` to generate ~/.cmdmind/cmdmind.sh with the correct binary path.

if [ -n "${CMDMIND_LOADED:-}" ]; then
  return 0 2>/dev/null || exit 0
fi
CMDMIND_LOADED=1

if [ -z "${CMDMIND_BIN:-}" ]; then
  CMDMIND_BIN=cmdmind
fi
export CMDMIND_BIN

__CMDMIND_CMD_STARTED=0
__CMDMIND_START_NS=0
__CMDMIND_LAST_CWD=""
__CMDMIND_LAST_HISTNO=0
__CMDMIND_IN_PROMPT=0

__CMDMIND_SUGGESTION=""
__CMDMIND_SUGGEST_PREFIX=""
__CMDMIND_SUGGEST_CWD=""
__CMDMIND_TAB_BOUND=0
__CMDMIND_TAB_BINDING=''

__cmdmind_now_ns() {
  date +%s%N 2>/dev/null || date +%s000000000
}

__cmdmind_message() {
  if [ -w /dev/tty ]; then
    printf "$@" > /dev/tty
  else
    printf "$@" >&2
  fi
}

__cmdmind_history_entry() {
  local hist
  hist="$(HISTTIMEFORMAT= builtin history 1)"
  if [[ "$hist" =~ ^[[:space:]]*([0-9]+)[[:space:]]+(.*)$ ]]; then
    printf '%s\t%s' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}"
  fi
}

__cmdmind_init_histno() {
  local entry hist_no
  entry="$(__cmdmind_history_entry)"
  hist_no="${entry%%$'\t'*}"
  if [ -n "$entry" ] && [ "$entry" != "$hist_no" ]; then
    __CMDMIND_LAST_HISTNO="$hist_no"
  fi
}

__cmdmind_capture_tab_binding() {
  local line
  __CMDMIND_TAB_BINDING=''
  while IFS= read -r line; do
    case "$line" in
      '"\C-i":'*|'"\t":'*) __CMDMIND_TAB_BINDING="$line"; return 0 ;;
    esac
  done < <(bind -p 2>/dev/null)
}

__cmdmind_restore_tab_binding() {
  if [ "${__CMDMIND_TAB_BOUND:-0}" = "1" ]; then
    if [ -n "${__CMDMIND_TAB_BINDING:-}" ]; then
      bind "$__CMDMIND_TAB_BINDING" 2>/dev/null || bind '"\C-i": complete' 2>/dev/null || true
    else
      bind '"\C-i": complete' 2>/dev/null || true
    fi
  fi
  __CMDMIND_TAB_BOUND=0
}

__cmdmind_bind_tab_accept() {
  if [ "${__CMDMIND_TAB_BOUND:-0}" != "1" ]; then
    __cmdmind_capture_tab_binding
    bind -x '"\C-i": __cmdmind_accept' 2>/dev/null || true
    __CMDMIND_TAB_BOUND=1
  fi
}

__cmdmind_clear_suggestion() {
  __CMDMIND_SUGGESTION=""
  __CMDMIND_SUGGEST_PREFIX=""
  __CMDMIND_SUGGEST_CWD=""
  __cmdmind_restore_tab_binding
}

__cmdmind_fetch() {
  local prefix cwd suggestion min_prefix
  prefix="$1"
  cwd="${2:-$PWD}"
  min_prefix="${CMDMIND_MIN_PREFIX:-2}"

  if [ "${CMDMIND_AUTOSUGGEST:-1}" = "0" ]; then
    return 1
  fi
  if [ -z "${prefix//[[:space:]]/}" ] || [ "${#prefix}" -lt "$min_prefix" ]; then
    return 1
  fi
  if [[ "$prefix" == *$'\n'* || "$prefix" == *$'\r'* ]]; then
    return 1
  fi
  if [ "$prefix" = "$__CMDMIND_SUGGEST_PREFIX" ] && [ "$cwd" = "$__CMDMIND_SUGGEST_CWD" ] && [ -n "$__CMDMIND_SUGGESTION" ]; then
    printf '%s' "$__CMDMIND_SUGGESTION"
    return 0
  fi

  suggestion="$(
    "$CMDMIND_BIN" suggest \
      --prefix "$prefix" \
      --cwd "$cwd" \
      --limit 1 \
      --fast 2>/dev/null | {
        IFS= read -r line
        printf '%s' "$line"
      }
  )"

  if [ -z "$suggestion" ] || [ "$suggestion" = "$prefix" ]; then
    __CMDMIND_SUGGESTION=""
    __CMDMIND_SUGGEST_PREFIX="$prefix"
    __CMDMIND_SUGGEST_CWD="$cwd"
    return 1
  fi
  if [[ "$suggestion" == *$'\n'* || "$suggestion" == *$'\r'* ]]; then
    __CMDMIND_SUGGESTION=""
    __CMDMIND_SUGGEST_PREFIX="$prefix"
    __CMDMIND_SUGGEST_CWD="$cwd"
    return 1
  fi

  __CMDMIND_SUGGESTION="$suggestion"
  __CMDMIND_SUGGEST_PREFIX="$prefix"
  __CMDMIND_SUGGEST_CWD="$cwd"
  printf '%s' "$suggestion"
}

__cmdmind_prefix_from_readline() {
  printf '%s' "${READLINE_LINE:0:READLINE_POINT}"
}

__cmdmind_accept() {
  local prefix suggestion after
  prefix="$(__cmdmind_prefix_from_readline)"
  suggestion="$__CMDMIND_SUGGESTION"
  if [ -z "$suggestion" ] || [ "$prefix" != "$__CMDMIND_SUGGEST_PREFIX" ]; then
    __cmdmind_restore_tab_binding
    return 0
  fi

  after="${READLINE_LINE:READLINE_POINT}"
  READLINE_LINE="${suggestion}${after}"
  READLINE_POINT=${#suggestion}
  __cmdmind_clear_suggestion
}

__cmdmind_plain_suggest() {
  local prefix suggestion suffix
  prefix="$(__cmdmind_prefix_from_readline)"
  suggestion="$(__cmdmind_fetch "$prefix" "$PWD")" || {
    __cmdmind_clear_suggestion
    __cmdmind_message '\ncmdmind: no suggestion\n'
    return 0
  }

  __cmdmind_bind_tab_accept
  if [[ "$suggestion" == "$prefix"* ]]; then
    suffix="${suggestion:${#prefix}}"
    __cmdmind_message '\ncmdmind: %s\033[2m\033[90m%s\033[0m  (Tab accept)\n' "$prefix" "$suffix"
  else
    __cmdmind_message '\ncmdmind: %s  (Tab accept)\n' "$suggestion"
  fi
}

__cmdmind_ble_source() {
  local prefix suggestion min_prefix
  [ "${CMDMIND_AUTOSUGGEST:-1}" != "0" ] || return 1
  [ "${_ble_edit_ind:-0}" -eq "${#_ble_edit_str}" ] || return 1
  [[ "$_ble_edit_str" != *$'\n'* && "$_ble_edit_str" != *$'\r'* ]] || return 1

  prefix="$_ble_edit_str"
  min_prefix="${CMDMIND_MIN_PREFIX:-2}"
  [ -n "${prefix//[[:space:]]/}" ] || return 1
  [ "${#prefix}" -ge "$min_prefix" ] || return 1

  suggestion="$(__cmdmind_fetch "$prefix" "$PWD")" || return 1
  [[ "$suggestion" == "$prefix"* && "$suggestion" != "$prefix" ]] || return 1

  ble/complete/auto-complete/enter h 0 "${suggestion:${#prefix}}" '' "$suggestion"
}

__cmdmind_setup_ble() {
  [ "${CMDMIND_UI:-auto}" != "plain" ] || return 1
  [ -n "${BLE_VERSION:-}" ] || return 1
  declare -F ble/complete/auto-complete/enter >/dev/null || return 1

  eval 'function ble/complete/auto-complete/source:cmdmind { __cmdmind_ble_source; }'

  local source found=0
  for source in "${_ble_complete_auto_source[@]}"; do
    [ "$source" = cmdmind ] && found=1 && break
  done
  [ "$found" = "1" ] || _ble_complete_auto_source=(cmdmind "${_ble_complete_auto_source[@]}")

  bleopt complete_auto_complete=1
  bleopt complete_auto_delay="${CMDMIND_DEBOUNCE_MS:-20}"
  ble-face -s auto_complete "${CMDMIND_GHOST_FACE:-fg=242}" 2>/dev/null || true
  ble-bind -m auto_complete -f C-i auto_complete/insert 2>/dev/null || true
  ble-bind -m auto_complete -f TAB auto_complete/insert 2>/dev/null || true
  return 0
}

__cmdmind_preexec() {
  local i
  if [ "${__CMDMIND_IN_PROMPT:-0}" = "1" ]; then
    return 0
  fi
  for ((i = 1; i < ${#FUNCNAME[@]}; i++)); do
    case "${FUNCNAME[$i]}" in
      __cmdmind_*) return 0 ;;
    esac
  done
  case "$BASH_COMMAND" in
    __cmdmind_*|cmdmind*|"$CMDMIND_BIN"*|history*|bind*|trap*|PROMPT_COMMAND*) return 0 ;;
  esac
  if [ "${__CMDMIND_CMD_STARTED:-0}" = "0" ]; then
    __CMDMIND_CMD_STARTED=1
    __CMDMIND_LAST_CWD="$PWD"
    __CMDMIND_START_NS="$(__cmdmind_now_ns)"
  fi
}

__cmdmind_precmd() {
  local exit_code=$?
  __CMDMIND_IN_PROMPT=1

  if [ "${__CMDMIND_CMD_STARTED:-0}" = "1" ]; then
    local entry hist_no cmd now_ns duration_ms
    entry="$(__cmdmind_history_entry)"
    hist_no="${entry%%$'\t'*}"
    cmd=""
    if [ -n "$entry" ] && [ "$entry" != "$hist_no" ] && [ "$hist_no" != "${__CMDMIND_LAST_HISTNO:-0}" ]; then
      cmd="${entry#*$'\t'}"
      __CMDMIND_LAST_HISTNO="$hist_no"
    fi
    now_ns="$(__cmdmind_now_ns)"
    duration_ms=0
    if [[ "$now_ns" =~ ^[0-9]+$ && "$__CMDMIND_START_NS" =~ ^[0-9]+$ && "$now_ns" -ge "$__CMDMIND_START_NS" ]]; then
      duration_ms=$(( (now_ns - __CMDMIND_START_NS) / 1000000 ))
    fi

    if [ -n "$cmd" ]; then
      "$CMDMIND_BIN" record \
        --cmd "$cmd" \
        --cwd "${__CMDMIND_LAST_CWD:-$PWD}" \
        --exit-code "$exit_code" \
        --duration-ms "$duration_ms" \
        --shell bash >/dev/null 2>&1 || true
    fi
  fi

  __CMDMIND_CMD_STARTED=0
  __CMDMIND_LAST_CWD=""
  __CMDMIND_START_NS=0
  __CMDMIND_IN_PROMPT=0
  __cmdmind_clear_suggestion
  __cmdmind_setup_ble >/dev/null 2>&1 || true
  return "$exit_code"
}

__cmdmind_init_histno

if [[ ";$PROMPT_COMMAND;" != *";__cmdmind_precmd;"* ]]; then
  PROMPT_COMMAND="__cmdmind_precmd${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
fi

trap '__cmdmind_preexec' DEBUG

bind -x '"\C-@": __cmdmind_plain_suggest' 2>/dev/null || true
bind -x '"\C- ": __cmdmind_plain_suggest' 2>/dev/null || true

__cmdmind_setup_ble >/dev/null 2>&1 || true
