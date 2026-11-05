-- The requests of autoload/goda.vim in Neovim with a timeout: vim.rpcrequest()
-- takes {opts} with {timeout} from Neovim 0.13 on.
local M = {}

-- Whether vim.rpcrequest() takes {opts}: the older one says that the method
-- (the table) is no string.
function M.has_timeout()
  local ok, err = pcall(vim.rpcrequest, -1, { timeout = 1 }, 'goda.hello')
  return not ok and not tostring(err):find('string expected', 1, true)
end

-- {true, result}, or {false, message}: the message of a timeout starts with
-- "Request timed out". No timeout when {timeout} is not positive.
function M.request(chan, timeout, name, args)
  local ok, r
  if timeout and timeout > 0 then
    ok, r = pcall(vim.rpcrequest, chan, { timeout = timeout }, name, unpack(args))
  else
    ok, r = pcall(vim.rpcrequest, chan, name, unpack(args))
  end
  if ok then
    return { true, r == nil and vim.NIL or r }
  end
  return { false, tostring(r) }
end

return M
