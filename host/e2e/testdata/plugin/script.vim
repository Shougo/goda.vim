" The Vim script of the plugin "test", embedded (sdk/vimscript).

function! godatest#script#hello(name) abort
  return s:greet(a:name)
endfunction

function! s:greet(name) abort
  return 'hello, '
        \ . a:name
endfunction
