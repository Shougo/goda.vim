" Driven by e2e_test.go: runs in a real Vim or Neovim and writes "name<TAB>result"
" lines to $GODA_OUT for the Go test to check.
" 'encoding' defaults to latin1 under -u NONE in a non-UTF-8 locale, which would
" garble the Japanese test string. Real setups use UTF-8.
set encoding=utf-8
scriptencoding utf-8
set nocompatible
set runtimepath^=$GODA_RTP

let s:out = []

function! s:rec(name, Fn) abort
  try
    call add(s:out, a:name . "\t" . json_encode(call(a:Fn, [])))
  catch
    call add(s:out, a:name . "\tEXC " . v:exception)
  endtry
endfunction

" A short request timeout, to see what a plugin that answers too late does.
let g:goda#request_timeout = 500

try
  call add(s:out, "start\t" . json_encode(goda#start($GODA_BIN)))
catch
  call add(s:out, "start\tEXC " . v:exception)
endtry
call add(s:out, "running\t" . goda#is_running('test'))

" Starting the same binary again is a no-op; a second, independent binary
" runs next to the first; a copy of the first (same plugin name) is refused;
" a binary speaking another protocol is refused and not left running.
call s:rec('start_again', {-> goda#start($GODA_BIN)})
call s:rec('start_other', {-> goda#start($GODA_BIN2)})
call s:rec('conflict', {-> goda#start($GODA_BIN_COPY)})
call s:rec('old_protocol', {-> goda#start($GODA_BIN_OLD)})
" A binary that never answers (not a goda.vim binary) must not hang the editor.
call s:rec('silent', {-> goda#start($GODA_BIN_SILENT)})
" A binary that gives up at once (a configuration error, say): its stderr is
" in the exception and in goda#log().
call s:rec('exits', {-> goda#start($GODA_BIN_EXITS)})
call add(s:out, "exits_log\t" . json_encode(goda#log()))
call s:rec('plugins', {-> sort(keys(goda#plugins()))})
call s:rec('reserved', {-> goda#request('goda', 'plugins')})
call s:rec('other_ping', {-> goda#request('other', 'ping')})
call s:rec('echo', {-> goda#request('test', 'echo', 1, 2.5, 'x', [v:true, v:null], {'k': ['日本語']})})
call s:rec('meta', {-> goda#request('test', 'meta')})

" The plugin calls back into this Vim while Vim waits for the answer.
call s:rec('callback', {-> goda#request('test', 'callback')})
call s:rec('probe', {-> goda#request('test', 'probe')})
call s:rec('struct', {-> goda#request('test', 'struct')})
call s:rec('stdout', {-> goda#request('test', 'shout')})
call s:rec('notify_order', {-> goda#request('test', 'notify')})
call add(s:out, "notify_log\t" . json_encode(filter(goda#log(), 'v:val =~# "^\\[notify\\]"')))

call s:rec('unknown_method', {-> goda#request('test', 'nope')})
call s:rec('unknown_plugin', {-> goda#request('nope', 'echo')})

" Too late: Vim gives up after 500 ms, and the plugin must stay usable.
call s:rec('slow', {-> goda#request('test', 'slow')})
call s:rec('after_slow', {-> goda#request('test', 'echo', 'still alive')})

call s:rec('notify', {-> goda#notify('test', 'echo', 1)})
call s:rec('after_notify', {-> goda#request('test', 'echo', 'after notify')})

" Let the late answer of 'slow' arrive (2 s after it was asked) and the stderr
" callbacks run before reading the log.
sleep 2200m
call s:rec('after_late_answer', {-> goda#request('test', 'echo', 'after the late answer')})
call add(s:out, "log\t" . json_encode(goda#log()))

" Stopping one plugin's binary leaves the other alone.
call goda#stop('other')
sleep 100m
call add(s:out, "other_running\t" . goda#is_running('other'))
call s:rec('other_after_stop', {-> goda#request('other', 'ping')})
call s:rec('test_after_other_stop', {-> goda#request('test', 'echo', 'alive')})

call goda#stop()
sleep 100m
call add(s:out, "stopped\t" . goda#is_running())

call writefile(s:out, $GODA_OUT)
qa!
