" goda.vim - Go based dark powered framework for Vim/Neovim
"
" One plugin binary (built by goda-build) serves both editors; the editor picks
" the transport with --transport:
"
"   Neovim  jobstart({'rpc': v:true}), msgpack-rpc, rpcrequest()
"   Vim     job_start({'mode': 'json'}), JSON channel, ch_evalexpr()
"
" goda#request(), goda#notify() and goda#gather() hide the difference.

let s:nvim_chan = 0   " Neovim: channel id of the plugin job
let s:job = v:null    " Vim: the plugin job
let s:log = []        " what the plugin wrote to stderr

function! s:log_add(lines) abort
  call extend(s:log, filter(copy(a:lines), 'v:val !=# ""'))
  if len(s:log) > 200
    call remove(s:log, 0, len(s:log) - 201)
  endif
endfunction

function! s:nvim_on_stderr(job_id, data, event) abort
  call s:log_add(a:data)
endfunction

function! s:nvim_on_exit(job_id, code, event) abort
  let s:nvim_chan = 0
  call s:log_add(['goda: plugin exited with code ' . a:code])
endfunction

function! s:vim_on_stderr(channel, msg) abort
  call s:log_add([a:msg])
endfunction

function! s:vim_on_exit(job, status) abort
  if s:job is a:job
    let s:job = v:null
  endif
  call s:log_add(['goda: plugin exited with status ' . a:status])
endfunction

" The last lines the plugin binary wrote to stderr.
function! goda#log() abort
  return copy(s:log)
endfunction

function! goda#is_running() abort
  if has('nvim')
    return s:nvim_chan > 0
  endif
  return s:job isnot v:null && job_status(s:job) ==# 'run'
endfunction

function! s:ensure_running() abort
  if !goda#is_running()
    throw 'goda: not started (call goda#start() first)'
  endif
endfunction

" Start the plugin binary. Throws if it cannot be started.
function! goda#start(bin) abort
  if goda#is_running()
    return 1
  endif
  let l:bin = fnamemodify(expand(a:bin), ':p')
  if !executable(l:bin)
    throw 'goda: not executable: ' . l:bin
  endif
  let s:log = []

  if has('nvim')
    let l:chan = jobstart([l:bin, '--transport=nvim'], {
          \ 'rpc': v:true,
          \ 'on_stderr': function('s:nvim_on_stderr'),
          \ 'on_exit': function('s:nvim_on_exit'),
          \ })
    if l:chan <= 0
      throw 'goda: failed to start ' . l:bin
    endif
    let s:nvim_chan = l:chan
    return 1
  endif

  if !has('job') || !has('channel')
    throw 'goda: this Vim was built without +job/+channel'
  endif
  let s:job = job_start([l:bin, '--transport=vim'], {
        \ 'mode': 'json',
        \ 'err_mode': 'nl',
        \ 'err_cb': function('s:vim_on_stderr'),
        \ 'exit_cb': function('s:vim_on_exit'),
        \ 'timeout': get(g:, 'goda_request_timeout', 10000),
        \ 'noblock': 1,
        \ })
  if job_status(s:job) !=# 'run'
    let s:job = v:null
    throw 'goda: failed to start ' . l:bin
  endif
  return 1
endfunction

function! goda#stop() abort
  if has('nvim')
    if s:nvim_chan > 0
      call jobstop(s:nvim_chan)
      let s:nvim_chan = 0
    endif
  elseif s:job isnot v:null
    call job_stop(s:job)
    let s:job = v:null
  endif
endfunction

" Blocking request to the plugin. Throws on failure.
function! goda#request(method, ...) abort
  call s:ensure_running()
  if has('nvim')
    return call('rpcrequest', [s:nvim_chan, a:method] + a:000)
  endif
  let l:r = ch_evalexpr(job_getchannel(s:job), [a:method] + a:000)
  if type(l:r) != v:t_dict
    throw 'goda: no answer to ' . a:method . ' (timed out, or the plugin exited)'
  endif
  if has_key(l:r, 'error')
    throw 'goda: ' . l:r.error
  endif
  return get(l:r, 'result', v:null)
endfunction

" Fire-and-forget notification to the plugin.
function! goda#notify(method, ...) abort
  call s:ensure_running()
  if has('nvim')
    return call('rpcnotify', [s:nvim_chan, a:method] + a:000)
  endif
  " id 0 tells the plugin that nobody waits for an answer
  call ch_sendraw(job_getchannel(s:job), json_encode([0, [a:method] + a:000]) . "\n")
endfunction

" Run the completion pipeline. See sdk.GatherRequest in the Go SDK:
"   {'sources': [...], 'filters': [...], 'context': {...}, 'timeout_ms': 300}
function! goda#gather(request) abort
  return goda#request('goda_gather', a:request)
endfunction

" Convenience: build a context from the current buffer around the cursor.
function! goda#context() abort
  let l:lnum = line('.')
  let l:first = max([1, l:lnum - 200])
  let l:last = min([line('$'), l:lnum + 200])
  let l:before = strpart(getline('.'), 0, col('.') - 1)
  return {
        \ 'input': l:before,
        \ 'complete_str': matchstr(l:before, '\k*$'),
        \ 'filetype': &filetype,
        \ 'line': l:lnum - l:first + 1,
        \ 'lines': getline(l:first, l:last),
        \ }
endfunction

" ---------------------------------------------------------------------------
" Helpers called BY the plugin binary on the Vim transport (host/vim). Vim
" reports a failed call as the bare string "ERROR", which cannot be told apart
" from a function returning "ERROR", so errors are caught here and answered as
" [0, result] or [1, message]. Not part of the public API.

function! goda#_call(fn, args) abort
  try
    return [0, call(a:fn, a:args)]
  catch
    return [1, v:exception]
  endtry
endfunction

function! goda#_eval(expr) abort
  try
    return [0, eval(a:expr)]
  catch
    return [1, v:exception]
  endtry
endfunction

function! goda#_cmd(cmd) abort
  try
    execute a:cmd
    return [0, v:null]
  catch
    return [1, v:exception]
  endtry
endfunction
