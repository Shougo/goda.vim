" goda#build() with g:goda#workspace, run by TestBuild: the results go to
" $GODA_OUT, one "name<Tab>value" a line.
set nocompatible
execute 'set runtimepath^=' . fnameescape($GODA_RTP)

let s:out = []
function! s:put(name, value) abort
  call add(s:out, a:name . "\t" . (type(a:value) == v:t_string ? a:value : string(a:value)))
endfunction

let s:bin = $GODA_BUILD_DIR . '/bin/cfg' . (has('win32') ? '.exe' : '')
let s:opts = #{name: 'cfg', dir: $GODA_BUILD_DIR . '/cfg', bin: s:bin}
let $GOWORK = 'kept'

" Without the workspace: example.com/godalib cannot be downloaded.
try
  call goda#build(s:opts)
  call s:put('no_workspace', 'built')
catch
  call s:put('no_workspace', v:exception =~# 'goda-cfg: go build failed' ? 'failed' : v:exception)
endtry
call s:put('no_workspace_bin', filereadable(s:bin))

" Without the workspace, a go.work of the user above the configuration is
" not used either: it would give example.com/godalib.
call writefile(['go 1.22.1', 'use (', "\t./cfg", "\t./lib", ')'], $GODA_BUILD_DIR . '/go.work')
unlet $GOWORK
try
  call goda#build(s:opts)
  call s:put('user_go_work', trim(system(shellescape(s:bin))))
catch
  call s:put('user_go_work', v:exception =~# 'goda-cfg: go build failed' ? 'failed' : v:exception)
endtry
call s:put('user_go_work_bin', filereadable(s:bin))
call delete($GODA_BUILD_DIR . '/go.work')
let $GOWORK = 'kept'

" The workspace: the module the configuration requires is used; the other
" one (whose go.mod cannot be resolved) is not.
let g:goda#workspace = [$GODA_BUILD_DIR . '/other', $GODA_BUILD_DIR . '/lib', $GODA_BUILD_DIR . '/lib2']
try
  call goda#build(s:opts)
  call s:put('workspace', trim(system(shellescape(s:bin))))
catch
  call s:put('workspace', v:exception)
endtry
call s:put('gowork_restored', $GOWORK)
let s:work = readfile($GODA_BUILD_DIR . '/bin/go.work')
call s:put('go_version', filter(copy(s:work), 'v:val =~# "^go "'))
call s:put('uses', len(filter(copy(s:work), 'v:val =~# "^\t"')))
call s:put('other_used', len(filter(copy(s:work), 'v:val =~# "other"')))

" A binary that runs (not stopped by goda#build()): it is replaced all the
" same (on MS-Windows, it is moved aside), and what was moved aside is
" removed by the next build, once it no longer runs.
if has('nvim')
  let s:job = jobstart([s:bin, 'wait'])
else
  let s:job = job_start([s:bin, 'wait'])
endif
sleep 500m
try
  call goda#build(s:opts)
  call s:put('running', trim(system(shellescape(s:bin))))
catch
  call s:put('running', v:exception)
endtry
call s:put('running_tmp', getftype(s:bin . '.new' . (has('win32') ? '.exe' : '')) !=# '')
if has('nvim')
  call jobstop(s:job)
  call jobwait([s:job], 5000)
else
  call job_stop(s:job, 'kill')
  for s:i in range(500)
    if job_status(s:job) !=# 'run'
      break
    endif
    sleep 10m
  endfor
endif
sleep 500m
try
  call goda#build(s:opts)
catch
  call s:put('rebuilt', v:exception)
endtry
call s:put('aside_removed', readdir($GODA_BUILD_DIR . '/bin', {n -> n =~# '\.old'}))

call writefile(s:out, $GODA_OUT)
qall!
