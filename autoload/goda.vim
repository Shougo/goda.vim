" goda.vim - Go based dark powered framework for Vim/Neovim
"
" Every plugin binary (built by the user, see README) is an independent
" process; any number of them can run side by side. A binary names the plugins
" it contains, and a request is routed to the binary that serves the plugin:
" goda#request('cmp', 'gather', {...}) goes to whichever binary contains 'cmp'.
"
" The editor picks the transport with --transport:
"
"   Neovim  jobstart({'rpc': v:true}), msgpack-rpc, rpcrequest()
"   Vim     job_start({'mode': 'json'}), JSON channel, ch_evalexpr()
"
" goda#request() and goda#notify() hide the difference.

" Must equal sdk.Protocol in the Go module (a test checks it). Bumped only when
" an old binary and a new autoload can no longer work together.
let s:protocol = 1

let s:instances = {}  " binary path -> instance
let s:routes = {}     " plugin name -> instance
" An instance is {'bin', 'job' (Vim), 'chan' (Neovim), 'log', 'plugins'}.

function! s:log_add(inst, lines) abort
  call extend(a:inst.log, filter(copy(a:lines), 'v:val !=# ""'))
  if len(a:inst.log) > 200
    call remove(a:inst.log, 0, len(a:inst.log) - 201)
  endif
endfunction

function! s:nvim_on_stderr(inst, job_id, data, event) abort
  call s:log_add(a:inst, a:data)
endfunction

function! s:nvim_on_exit(inst, job_id, code, event) abort
  let a:inst.chan = 0
  call s:log_add(a:inst, ['goda: ' . a:inst.bin . ' exited with code ' . a:code])
endfunction

function! s:vim_on_stderr(inst, channel, msg) abort
  call s:log_add(a:inst, [a:msg])
endfunction

function! s:vim_on_exit(inst, job, status) abort
  if a:inst.job is a:job
    let a:inst.job = v:null
  endif
  call s:log_add(a:inst, ['goda: ' . a:inst.bin . ' exited with status ' . a:status])
endfunction

function! s:alive(inst) abort
  if has('nvim')
    return a:inst.chan > 0
  endif
  return a:inst.job isnot v:null && job_status(a:inst.job) ==# 'run'
endfunction

function! s:kill(inst) abort
  if has('nvim')
    if a:inst.chan > 0
      call jobstop(a:inst.chan)
      let a:inst.chan = 0
    endif
  elseif a:inst.job isnot v:null
    call job_stop(a:inst.job)
    let a:inst.job = v:null
  endif
endfunction

" Blocking call of a method of one binary, by its full name ('plugin.method').
function! s:rpc(inst, name, args) abort
  if has('nvim')
    return call('rpcrequest', [a:inst.chan, a:name] + a:args)
  endif
  let l:r = ch_evalexpr(job_getchannel(a:inst.job), [a:name] + a:args)
  if type(l:r) != v:t_dict
    throw 'goda: no answer to ' . a:name . ' (timed out, or the plugin exited)'
  endif
  if has_key(l:r, 'error')
    throw 'goda: ' . l:r.error
  endif
  return get(l:r, 'result', v:null)
endfunction

function! s:route(plugin) abort
  if a:plugin ==# 'goda'
    throw 'goda: the plugin name "goda" is reserved (use goda#plugins())'
  endif
  let l:inst = get(s:routes, a:plugin, v:null)
  if l:inst is v:null
    throw 'goda: unknown plugin "' . a:plugin . '" (is its binary started? see goda#plugins())'
  endif
  if !s:alive(l:inst)
    throw 'goda: ' . l:inst.bin . ' (plugin "' . a:plugin . '") is not running; see goda#log("' . a:plugin . '")'
  endif
  return l:inst
endfunction

" ---------------------------------------------------------------------------
" Public API

" Start a plugin binary and return the names of the plugins it contains. Safe
" to call again for a binary that is already running. Throws if the binary
" cannot be started, speaks another protocol version (rebuild it), or contains
" a plugin whose name another running binary already serves.
function! goda#start(bin) abort
  let l:bin = fnamemodify(expand(a:bin), ':p')
  if !executable(l:bin)
    throw 'goda: not executable: ' . l:bin
  endif
  let l:old = get(s:instances, l:bin, v:null)
  if l:old isnot v:null && s:alive(l:old)
    return copy(l:old.plugins)
  endif

  let l:inst = {'bin': l:bin, 'job': v:null, 'chan': 0, 'log': [], 'plugins': []}
  if has('nvim')
    let l:chan = jobstart([l:bin, '--transport=nvim'], {
          \ 'rpc': v:true,
          \ 'on_stderr': function('s:nvim_on_stderr', [l:inst]),
          \ 'on_exit': function('s:nvim_on_exit', [l:inst]),
          \ })
    if l:chan <= 0
      throw 'goda: failed to start ' . l:bin
    endif
    let l:inst.chan = l:chan
  else
    if !has('job') || !has('channel')
      throw 'goda: this Vim was built without +job/+channel'
    endif
    let l:inst.job = job_start([l:bin, '--transport=vim'], {
          \ 'mode': 'json',
          \ 'err_mode': 'nl',
          \ 'err_cb': function('s:vim_on_stderr', [l:inst]),
          \ 'exit_cb': function('s:vim_on_exit', [l:inst]),
          \ 'timeout': get(g:, 'goda#request_timeout', 10000),
          \ 'noblock': 1,
          \ })
    if job_status(l:inst.job) !=# 'run'
      throw 'goda: failed to start ' . l:bin
    endif
  endif

  " Handshake: protocol version and the plugins the binary contains.
  try
    let l:hello = s:rpc(l:inst, 'goda.hello', [])
  catch
    let l:msg = v:exception
    call s:kill(l:inst)
    throw 'goda: handshake with ' . l:bin . ' failed (built with an older goda.vim? rebuild it): ' . l:msg
  endtry
  let l:theirs = type(l:hello) == v:t_dict ? get(l:hello, 'protocol', 0) : 0
  if l:theirs != s:protocol
    call s:kill(l:inst)
    throw 'goda: ' . l:bin . ' speaks protocol ' . l:theirs . ', this goda.vim expects ' . s:protocol
          \ . '; rebuild the binary (or update goda.vim)'
  endif
  let l:plugins = sort(keys(l:hello.plugins))
  for l:name in l:plugins
    let l:other = get(s:routes, l:name, v:null)
    if l:other isnot v:null && l:other.bin !=# l:bin && s:alive(l:other)
      call s:kill(l:inst)
      throw 'goda: plugin "' . l:name . '" of ' . l:bin . ' is already served by ' . l:other.bin
    endif
  endfor

  let l:inst.plugins = l:plugins
  let l:inst.version = get(l:hello, 'version', '')
  let s:instances[l:bin] = l:inst
  for l:name in l:plugins
    let s:routes[l:name] = l:inst
  endfor
  return copy(l:plugins)
endfunction

" Stop the binary that serves PLUGIN, or all binaries without an argument.
function! goda#stop(...) abort
  if a:0 == 0
    for l:inst in values(s:instances)
      call s:kill(l:inst)
    endfor
    let s:instances = {}
    let s:routes = {}
    return
  endif
  let l:inst = get(s:routes, a:1, v:null)
  if l:inst is v:null
    return
  endif
  call s:kill(l:inst)
  for l:name in l:inst.plugins
    if get(s:routes, l:name, v:null) is l:inst
      call remove(s:routes, l:name)
    endif
  endfor
  if get(s:instances, l:inst.bin, v:null) is l:inst
    call remove(s:instances, l:inst.bin)
  endif
endfunction

" Running plugins: {name: binary path}.
function! goda#plugins() abort
  let l:r = {}
  for [l:name, l:inst] in items(s:routes)
    if s:alive(l:inst)
      let l:r[l:name] = l:inst.bin
    endif
  endfor
  return l:r
endfunction

" Is the binary serving PLUGIN running? Without an argument: is any running?
function! goda#is_running(...) abort
  if a:0 == 0
    return !empty(goda#plugins())
  endif
  let l:inst = get(s:routes, a:1, v:null)
  return l:inst isnot v:null && s:alive(l:inst)
endfunction

" What the binary serving PLUGIN wrote to stderr (it is kept after the binary
" exited). Without an argument: every binary's lines, prefixed with its name.
function! goda#log(...) abort
  if a:0 > 0
    let l:inst = get(s:routes, a:1, v:null)
    return l:inst is v:null ? [] : copy(l:inst.log)
  endif
  let l:all = []
  for l:inst in values(s:instances)
    let l:tag = '[' . fnamemodify(l:inst.bin, ':t') . '] '
    call extend(l:all, map(copy(l:inst.log), 'l:tag . v:val'))
  endfor
  return l:all
endfunction

" Blocking request to a plugin's method. Throws on failure.
function! goda#request(plugin, method, ...) abort
  return s:rpc(s:route(a:plugin), a:plugin . '.' . a:method, a:000)
endfunction

" Fire-and-forget notification to a plugin's method.
function! goda#notify(plugin, method, ...) abort
  let l:inst = s:route(a:plugin)
  let l:name = a:plugin . '.' . a:method
  if has('nvim')
    return call('rpcnotify', [l:inst.chan, l:name] + a:000)
  endif
  " id 0 tells the plugin that nobody waits for an answer
  call ch_sendraw(job_getchannel(l:inst.job), json_encode([0, [l:name] + a:000]) . "\n")
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
