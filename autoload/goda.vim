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
let s:failed = {}     " binary path -> instance whose start failed (for goda#log())
let s:notify_errors = []  " errors of functions plugins called with Notify
" An instance is {'bin', 'job' (Vim), 'chan' and 'job_id' (Neovim), 'log',
" 'plugins', 'status'}. status is the exit status once the process ended,
" v:null before.

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
  let a:inst.status = a:code
  call s:log_add(a:inst, ['goda: ' . a:inst.bin . ' exited with code ' . a:code])
endfunction

function! s:vim_on_stderr(inst, channel, msg) abort
  call s:log_add(a:inst, [a:msg])
endfunction

function! s:vim_on_exit(inst, job, status) abort
  if a:inst.job is a:job
    let a:inst.job = v:null
  endif
  let a:inst.status = a:status
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

" Waits up to TIMEOUT ms for the process to end, and returns its exit status,
" or v:null when it is still running. Its stderr has been read by then.
function! s:wait_exit(inst, timeout) abort
  if has('nvim')
    " jobwait() also runs the on_stderr and on_exit callbacks.
    let r = jobwait([a:inst.job_id], a:timeout)[0]
    if r == -1
      return v:null
    endif
    return a:inst.status is v:null ? r : a:inst.status
  endif
  let waited = 0
  while a:inst.status is v:null && waited < a:timeout
    " job_status() notices the end and runs exit_cb; sleeping lets the
    " err_cb lines in.
    if a:inst.job isnot v:null && job_status(a:inst.job) !=# 'run'
      break
    endif
    sleep 10m
    let waited += 10
  endwhile
  if a:inst.job isnot v:null
    call job_status(a:inst.job)
  endif
  " The last stderr lines may still be on their way.
  sleep 20m
  return a:inst.status
endfunction

" Blocking call of a method of one binary, by its full name ('plugin.method').
function! s:rpc(inst, name, args) abort
  if has('nvim')
    return call('rpcrequest', [a:inst.chan, a:name] + a:args)
  endif
  let r = ch_evalexpr(job_getchannel(a:inst.job), [a:name] + a:args)
  if type(r) != v:t_dict
    throw 'goda: no answer to ' . a:name . ' (timed out, or the plugin exited)'
  endif
  if has_key(r, 'error')
    throw 'goda: ' . r.error
  endif
  return get(r, 'result', v:null)
endfunction

function! s:route(plugin) abort
  if a:plugin ==# 'goda'
    throw 'goda: the plugin name "goda" is reserved (use goda#plugins())'
  endif
  let inst = get(s:routes, a:plugin, v:null)
  if inst is v:null
    throw 'goda: unknown plugin "' . a:plugin . '" (is its binary started? see goda#plugins())'
  endif
  if !s:alive(inst)
    throw 'goda: ' . inst.bin . ' (plugin "' . a:plugin . '") is not running; see goda#log("' . a:plugin . '")'
  endif
  return inst
endfunction

" ---------------------------------------------------------------------------
" Public API

" Start a plugin binary and return the names of the plugins it contains. Safe
" to call again for a binary that is already running. Throws if the binary
" cannot be started, speaks another protocol version (update and rebuild it), or contains
" a plugin whose name another running binary already serves.
function! goda#start(bin) abort
  let bin = fnamemodify(expand(a:bin), ':p')
  if !executable(bin)
    throw 'goda: not executable: ' . bin
  endif
  let old = get(s:instances, bin, v:null)
  if old isnot v:null && s:alive(old)
    return copy(old.plugins)
  endif

  let inst = {'bin': bin, 'job': v:null, 'chan': 0, 'job_id': 0, 'log': [], 'plugins': [], 'status': v:null}
  if has('nvim')
    let chan = jobstart([bin, '--transport=nvim'], {
          \ 'rpc': v:true,
          \ 'on_stderr': function('s:nvim_on_stderr', [inst]),
          \ 'on_exit': function('s:nvim_on_exit', [inst]),
          \ })
    if chan <= 0
      throw 'goda: failed to start ' . bin
    endif
    let inst.chan = chan
    let inst.job_id = chan
  else
    if !has('job') || !has('channel')
      throw 'goda: this Vim was built without +job/+channel'
    endif
    let inst.job = job_start([bin, '--transport=vim'], {
          \ 'mode': 'json',
          \ 'err_mode': 'nl',
          \ 'err_cb': function('s:vim_on_stderr', [inst]),
          \ 'exit_cb': function('s:vim_on_exit', [inst]),
          \ 'timeout': get(g:, 'goda#request_timeout', 10000),
          \ 'noblock': 1,
          \ })
    if job_status(inst.job) !=# 'run'
      throw 'goda: failed to start ' . bin
    endif
  endif

  " Handshake: protocol version and the plugins the binary contains.
  try
    let hello = s:rpc(inst, 'goda.hello', [])
  catch
    let msg = v:exception
    let status = s:wait_exit(inst, 1000)
    call s:kill(inst)
    let s:failed[bin] = inst
    if status isnot v:null
      " It ended by itself (a configuration error, a crash): what it wrote
      " to stderr says why.
      let lines = filter(copy(inst.log), 'v:val !~# "^goda: .* exited with "')
      throw 'goda: ' . bin . ' exited during the handshake (status ' . status . ')'
            \ . (empty(lines) ? ', without a message' : ":\n  " . join(lines, "\n  "))
    endif
    throw 'goda: handshake with ' . bin . ' failed: ' . msg
          \ . "\n  The binary was probably built with an older goda.vim module. Update it in the Go module"
          \ . " that builds the binary (go get -u github.com/Shougo/goda.vim) and build again."
  endtry
  if has_key(s:failed, bin)
    call remove(s:failed, bin)
  endif
  let theirs = type(hello) == v:t_dict ? get(hello, 'protocol', 0) : 0
  if theirs != s:protocol
    call s:kill(inst)
    throw 'goda: ' . bin . ' speaks protocol ' . theirs . ', this goda.vim expects ' . s:protocol
          \ . '; update the goda.vim module in the Go module that builds the binary'
          \ . ' (go get -u github.com/Shougo/goda.vim) and build again, or update the goda.vim plugin'
  endif
  let plugins = sort(keys(hello.plugins))
  for name in plugins
    let other = get(s:routes, name, v:null)
    if other isnot v:null && other.bin !=# bin && s:alive(other)
      call s:kill(inst)
      throw 'goda: plugin "' . name . '" of ' . bin . ' is already served by ' . other.bin
    endif
  endfor

  let inst.plugins = plugins
  let inst.version = get(hello, 'version', '')
  let s:instances[bin] = inst
  for name in plugins
    let s:routes[name] = inst
  endfor
  return copy(plugins)
endfunction

" Stop the binary that serves PLUGIN, or all binaries without an argument.
function! goda#stop(...) abort
  if a:0 == 0
    for inst in values(s:instances)
      call s:kill(inst)
    endfor
    let s:instances = {}
    let s:routes = {}
    return
  endif
  let inst = get(s:routes, a:1, v:null)
  if inst is v:null
    return
  endif
  call s:kill(inst)
  for name in inst.plugins
    if get(s:routes, name, v:null) is inst
      call remove(s:routes, name)
    endif
  endfor
  if get(s:instances, inst.bin, v:null) is inst
    call remove(s:instances, inst.bin)
  endif
endfunction

" Running plugins: {name: binary path}.
function! goda#plugins() abort
  let r = {}
  for [name, inst] in items(s:routes)
    if s:alive(inst)
      let r[name] = inst.bin
    endif
  endfor
  return r
endfunction

" Is the binary serving PLUGIN running? Without an argument: is any running?
function! goda#is_running(...) abort
  if a:0 == 0
    return !empty(goda#plugins())
  endif
  let inst = get(s:routes, a:1, v:null)
  return inst isnot v:null && s:alive(inst)
endfunction

" What the binary serving PLUGIN wrote to stderr (it is kept after the binary
" exited). Without an argument: every binary's lines, prefixed with its name.
function! goda#log(...) abort
  if a:0 > 0
    let inst = get(s:routes, a:1, v:null)
    return inst is v:null ? [] : copy(inst.log)
  endif
  let all = map(copy(s:notify_errors), '"[notify] " . v:val')
  " Binaries whose start failed come first: they are what to look at.
  for inst in values(s:failed) + values(s:instances)
    let tag = '[' . fnamemodify(inst.bin, ':t') . '] '
    call extend(all, map(copy(inst.log), 'tag . v:val'))
  endfor
  return all
endfunction

" Blocking request to a plugin's method. Throws on failure.
function! goda#request(plugin, method, ...) abort
  return s:rpc(s:route(a:plugin), a:plugin . '.' . a:method, a:000)
endfunction

" Fire-and-forget notification to a plugin's method.
function! goda#notify(plugin, method, ...) abort
  let inst = s:route(a:plugin)
  let name = a:plugin . '.' . a:method
  if has('nvim')
    return call('rpcnotify', [inst.chan, name] + a:000)
  endif
  " id 0 tells the plugin that nobody waits for an answer
  call ch_sendraw(job_getchannel(inst.job), json_encode([0, [name] + a:000]) . "\n")
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

" sdk.Host.Notify: nobody waits for the result, so an error is kept for
" goda#log() instead of being answered.
function! goda#_notify(fn, args) abort
  try
    call call(a:fn, a:args)
  catch
    call add(s:notify_errors, a:fn . ': ' . v:exception)
    if len(s:notify_errors) > 100
      call remove(s:notify_errors, 0)
    endif
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
