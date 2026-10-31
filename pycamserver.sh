mpv --no-cache --untimed \
  --demuxer-lavf-format=h264 \
  --external-file=tcp://10.0.4.67:9999 \
  --lavfi-complex='[vid1][vid2]hstack=inputs=2[vo]' \
  tcp://10.0.4.60:9999