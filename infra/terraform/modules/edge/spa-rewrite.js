// Paths without a file extension are client-side routes and get index.html.
function handler(event) {
  var request = event.request;
  var uri = request.uri;
  if (uri === '/' || uri.lastIndexOf('.') > uri.lastIndexOf('/')) {
    return request;
  }
  request.uri = '/index.html';
  return request;
}
