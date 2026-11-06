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
call s:put('uses', len(filter(copy(s:work), 'v:val =~# "^\t\""')))
call s:put('other_used', len(filter(copy(s:work), 'v:val =~# "other"')))

call writefile(s:out, $GODA_OUT)
qall!
