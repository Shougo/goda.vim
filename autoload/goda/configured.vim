" The setup of a plugin whose binary is built from the user's configuration
" (a Go module), as goda-cmp.vim, goda-fi.vim and the others: where the
" configuration and the binary are, how to build the binary, and how
" goda.vim starts it.
"
" For the plugin "foo" (its autoload script goda_foo.vim):
"
"   g:goda_foo#config_dir  the configuration, by default
"                          {config}/goda-foo ({config}: stdpath('config')
"                          of Neovim, ~/.vim of Vim)
"   g:goda_foo#binary      a binary the user builds; by default the binary
"                          is {cache}/goda-foo/goda-foo, built by goda.vim
"                          when there is none or it is of another version
"   goda_foo#build()       the function of the plugin that builds it (the
"                          errors say to call it)

" A plugin set up by NAME; OPTS are "version" and "module" (those of
" goda#register()), "status": whether the binary is asked "status" as it
" starts (v:true by default), and "dir": the module built when
" g:goda_foo#config_dir is not set, instead of {config}/goda-foo (a plugin
" with no configuration of the user builds its own directory). The plugin
" has:
"
"   config_dir()      the directory of the configuration
"   binary()          the binary
"   build()           goda#build() of the configuration into the binary
"   register([starter])
"                     goda#register() of the plugin: with "build" when the
"                     binary is not the user's (g:goda_foo#binary), and
"                     with start() as the starter by default
"   start()           start the binary: it must contain the plugin, and its
"                     "status" (if asked) must be "" (an invalid
"                     configuration builds a binary that says why instead)
function! goda#configured#new(name, opts = {}) abort
  if type(a:name) != v:t_string || a:name ==# ''
    throw 'goda: goda#configured#new(): the name is a String'
  endif
  if type(a:opts) != v:t_dict
    throw 'goda: goda#configured#new(): the options are a Dictionary'
  endif
  if has_key(a:opts, 'dir') && (type(a:opts.dir) != v:t_string || a:opts.dir ==# '')
    throw 'goda: goda#configured#new(): "dir" is a directory'
  endif
  let p = #{name: a:name,
        \ _version: filter(copy(a:opts), {k, v -> k ==# 'version' || k ==# 'module'}),
        \ _status: get(a:opts, 'status', v:true),
        \ _dir: get(a:opts, 'dir', '')}
  " (Partials of the plugin, not dict functions: goda#register() calls
  " "build" from a Dictionary of its own.)
  for method in ['config_dir', 'binary', 'build', 'register', 'start']
    let p[method] = function('s:' . method, [p])
  endfor
  return p
endfunction

function! s:var(p, name) abort
  return 'goda_' . a:p.name . '#' . a:name
endfunction

function! s:label(p) abort
  return 'goda-' . a:p.name
endfunction

function! s:config_dir(p) abort
  let var = s:var(a:p, 'config_dir')
  if has_key(g:, var)
    return expand(g:[var], 1)
  endif
  if a:p._dir !=# ''
    return a:p._dir
  endif
  return (has('nvim') ? stdpath('config') : expand('~/.vim', 1)) . '/' . s:label(a:p)
endfunction

" The binary is machine specific, so it is not built into the configuration
" directory (which belongs in the user's dotfiles) but into a cache directory.
function! s:cache_dir() abort
  if has('nvim')
    return stdpath('cache')
  elseif !empty($XDG_CACHE_HOME)
    return $XDG_CACHE_HOME . '/vim'
  elseif has('win32') && !empty($LOCALAPPDATA)
    return $LOCALAPPDATA . '/vim'
  endif
  return expand('~/.cache/vim', 1)
endfunction

" (A full path, as goda#start() makes it.)
function! s:binary(p) abort
  let var = s:var(a:p, 'binary')
  if has_key(g:, var)
    " (Not 'wildignore': '*.exe' in it on MS-Windows made it "".)
    return fnamemodify(expand(g:[var], 1), ':p')
  endif
  let label = s:label(a:p)
  return s:cache_dir() . '/' . label . '/' . label . (has('win32') ? '.exe' : '')
endfunction

function! s:build(p) abort
  call goda#build(#{name: a:p.name, dir: s:config_dir(a:p), bin: s:binary(a:p)})
endfunction

" (Again after a build: a start that failed is tried again.)
function! s:register(p, starter = v:null) abort
  let Starter = a:starter is v:null ? a:p.start : a:starter
  let opts = copy(a:p._version)
  " Not for the binary of the user: the user builds it.
  if !has_key(g:, s:var(a:p, 'binary'))
    call extend(opts, #{build: a:p.build, bin: s:binary(a:p)})
  endif
  call goda#register(a:p.name, Starter, opts)
endfunction

function! s:start(p) abort
  let name = a:p.name
  let label = s:label(a:p)
  let build = 'goda_' . name . '#build()'
  let bin = s:binary(a:p)
  if !executable(bin)
    throw label . ': ' . bin . ' does not exist yet; build it with: call ' . build
  endif
  let plugins = goda#start(bin)
  if index(plugins, name) < 0
    " Stop only the binary just started (through one of its plugins), not
    " the binaries of other plugins.
    if !empty(plugins)
      call goda#stop(plugins[0])
    endif
    throw label . ': ' . bin . ' does not contain the "' . name . '" plugin'
  endif
  if !a:p._status
    return
  endif
  let status = goda#request(name, 'status')
  if type(status) == v:t_string && status !=# ''
    call goda#stop(name)
    throw label . ': fix the configuration in ' . s:config_dir(a:p)
          \ . ' and run ' . build . ': ' . status
  endif
endfunction
