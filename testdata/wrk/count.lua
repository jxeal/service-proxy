codes = {}
function response(status, headers, body)
  local k = tostring(status)
  codes[k] = (codes[k] or 0) + 1
  if k ~= "200" and not bad_shown then
    io.write(string.format("FIRST-BAD status=%s body=%s\n", k, string.sub(body or "", 1, 120)))
    bad_shown = true
  end
end
