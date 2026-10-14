" Driven by vim_e2e_test.go: runs in a real Vim and writes "name<TAB>result"
" lines to $GODA_OUT for the Go test to check.
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

try
  call goda#start($GODA_BIN)
catch
  call add(s:out, "start\tEXC " . v:exception)
endtry
call add(s:out, "running\t" . goda#is_running())

call s:rec('list', {-> goda#request('goda_list')})

call s:rec('around', {-> goda#gather({
      \ 'sources': ['around'], 'filters': ['matcher_head'],
      \ 'context': {'complete_str': 'fo', 'line': 1, 'lines': ['foo bar foobar']}})})

" The plugin calls back into this Vim while Vim waits for the answer.
call s:rec('callback', {-> goda#gather({
      \ 'sources': ['vim'], 'filters': [], 'context': {'complete_str': 'strl'}})})

call s:rec('probe', {-> goda#gather({
      \ 'sources': ['probe'], 'filters': [], 'context': {}})})

call s:rec('slow', {-> goda#gather({
      \ 'sources': ['slow', 'around'], 'filters': [], 'timeout_ms': 150,
      \ 'context': {'complete_str': 'x', 'line': 1, 'lines': ['xray']}})})

call s:rec('stdout', {-> goda#gather({
      \ 'sources': ['shout'], 'filters': [], 'context': {}})})

call s:rec('unknown_method', {-> goda#request('nope')})
call s:rec('unknown_source', {-> goda#gather({
      \ 'sources': ['missing'], 'filters': [], 'context': {}})})

call s:rec('notify', {-> goda#notify('goda_list')})
call s:rec('after_notify', {-> goda#request('goda_list')})

" Let the stderr callbacks run before reading the log.
sleep 200m
call add(s:out, "log\t" . json_encode(goda#log()))

call goda#stop()
sleep 100m
call add(s:out, "stopped\t" . goda#is_running())

call writefile(s:out, $GODA_OUT)
qa!
