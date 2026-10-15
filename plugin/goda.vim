if exists('g:loaded_goda')
  finish
endif
let g:loaded_goda = 1

command! -nargs=1 -complete=file GodaStart call goda#start(<q-args>)
command! GodaStop call goda#stop()
command! GodaPlugins echo goda#request('goda', 'plugins')
command! GodaLog echo join(goda#log(), "\n")
