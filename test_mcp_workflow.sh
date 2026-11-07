#!/bin/sh

echo "=== Testing MCP Functionality with Real Files ==="
export ID3MCP_ROOT=/root/id3mcp

echo "1. Testing id3_list tool:"
echo '{"method":"id3_list","params":{}}' | ./id3mcp-binary
echo ""

echo "2. Testing id3_read tool on the MP3 file:"
echo '{"method":"id3_read","params":{"file":"1997.01.03 - ШБ.4.4.29.mp3"}}' | ./id3mcp-binary
echo ""

echo "3. Testing id3_set tool to set title and artist (preview mode):"
echo '{"method":"id3_set","params":{"file":"1997.01.03 - ШБ.4.4.29.mp3","title":"1997.01.03 - ШБ.4.4.29","artist":"Е.С. Бхакти Вигьяна Госвами Махарадж","confirm":false}}' | ./id3mcp-binary
echo ""

echo "4. Testing id3_set_cover tool to add cover (preview mode):"
echo '{"method":"id3_set_cover","params":{"file":"1997.01.03 - ШБ.4.4.29.mp3","cover":"cover.jpg","confirm":false}}' | ./id3mcp-binary
echo ""

echo "5. Actually applying the changes (setting title and artist):"
echo '{"method":"id3_set","params":{"file":"1997.01.03 - ШБ.4.4.29.mp3","title":"1997.01.03 - ШБ.4.4.29","artist":"Е.С. Бхакти Вигьяна Госвами Махарадж","confirm":true}}' | ./id3mcp-binary
echo ""

echo "6. Actually applying the cover:"
echo '{"method":"id3_set_cover","params":{"file":"1997.01.03 - ШБ.4.4.29.mp3","cover":"cover.jpg","confirm":true}}' | ./id3mcp-binary
echo ""

echo "7. Final verification - reading tags after changes:"
echo '{"method":"id3_read","params":{"file":"1997.01.03 - ШБ.4.4.29.mp3"}}' | ./id3mcp-binary
echo ""