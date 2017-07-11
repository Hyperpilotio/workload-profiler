var $deployments = $('.deployment-list')
$(function() {
    if ($deployments) {
        fixDeploymentHeight();
    }
});

function fixDeploymentHeight() {
    var realHeight = $deployments.height();
    $deployments.css('height', (realHeight + 100) + 'px');
}